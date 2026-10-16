package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

func geminiCookieHeader() (string, error) {
	root := filepath.Join(os.Getenv("APPDATA"), "gemini")
	key, err := geminiCookieKey(filepath.Join(root, "Local State"))
	if err != nil {
		return "", err
	}
	rows, err := readGeminiCookies(filepath.Join(root, "Network", "Cookies"))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	n := 0
	decryptFailed := false
	for _, row := range rows {
		if !geminiCookieAllowed(row.host, row.name) {
			continue
		}
		value, err := decryptGeminiCookie(key, row.encrypted, row.host, row.version)
		if err != nil || value == "" {
			decryptFailed = decryptFailed || err != nil
			continue
		}
		if n > 0 {
			b.WriteString("; ")
		}
		b.WriteString(row.name)
		b.WriteByte('=')
		b.WriteString(value)
		n++
		if n == 24 {
			break
		}
	}
	if n == 0 {
		if decryptFailed {
			return "", fmt.Errorf("Gemini 桌面版登入 cookie 無法解密或格式不支援")
		}
		return "", fmt.Errorf("Gemini 桌面版尚未登入")
	}
	return b.String(), nil
}

func sharingViolation(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == 32
}

func sharedFile(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func readShared(path string) ([]byte, error) {
	f, err := sharedFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<20))
}

func geminiCookieKey(path string) ([]byte, error) {
	data, err := readShared(path)
	if err != nil {
		return nil, fmt.Errorf("找不到 Gemini 桌面版登入資料")
	}
	var doc struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.OSCrypt.EncryptedKey == "" {
		return nil, fmt.Errorf("Gemini 桌面版登入資料無法讀取")
	}
	raw, err := base64.StdEncoding.DecodeString(doc.OSCrypt.EncryptedKey)
	if err != nil || !bytes.HasPrefix(raw, []byte("DPAPI")) {
		return nil, fmt.Errorf("Gemini 桌面版登入加密方式不支援")
	}
	key, err := cryptUnprotect(raw[len("DPAPI"):])
	if err != nil || len(key) == 0 {
		return nil, fmt.Errorf("Gemini 桌面版登入資料無法解密")
	}
	return key, nil
}

type geminiCookie struct {
	host, name string
	encrypted  []byte
	version    int
}

func readGeminiCookies(path string) ([]geminiCookie, error) {
	dir, err := os.MkdirTemp("", "aiusage-gemini-")
	if err != nil {
		return nil, fmt.Errorf("無法讀取 Gemini 桌面版登入")
	}
	defer os.RemoveAll(dir)
	for _, name := range []string{"Cookies", "Cookies-wal", "Cookies-shm"} {
		src := path
		if name != "Cookies" {
			src = path + "-" + strings.TrimPrefix(name, "Cookies-")
		}
		in, err := sharedFile(src)
		if err != nil {
			if name == "Cookies" {
				if sharingViolation(err) {
					return nil, fmt.Errorf("Gemini 桌面版正在使用登入資料，請先關閉桌面版再重新掃描")
				}
				return nil, fmt.Errorf("找不到 Gemini 桌面版登入資料")
			}
			continue
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = io.Copy(out, io.LimitReader(in, 32<<20))
		}
		in.Close()
		if out != nil {
			out.Close()
		}
		if err != nil && name == "Cookies" {
			return nil, fmt.Errorf("Gemini 桌面版登入資料無法複製")
		}
	}
	return queryGeminiCookies(filepath.Join(dir, "Cookies"))
}

func queryGeminiCookies(path string) ([]geminiCookie, error) {
	dll := syscall.NewLazyDLL("winsqlite3.dll")
	open := dll.NewProc("sqlite3_open_v2")
	closeDB := dll.NewProc("sqlite3_close")
	prepare := dll.NewProc("sqlite3_prepare_v2")
	step := dll.NewProc("sqlite3_step")
	finalize := dll.NewProc("sqlite3_finalize")
	columnText := dll.NewProc("sqlite3_column_text")
	columnBlob := dll.NewProc("sqlite3_column_blob")
	columnBytes := dll.NewProc("sqlite3_column_bytes")
	for _, proc := range []*syscall.LazyProc{open, closeDB, prepare, step, finalize, columnText, columnBlob, columnBytes} {
		if err := proc.Find(); err != nil {
			return nil, fmt.Errorf("Windows SQLite 無法載入")
		}
	}
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("Gemini 桌面版登入資料無法讀取")
	}
	var db uintptr
	rc, _, _ := open.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&db)), 1, 0)
	runtime.KeepAlive(p)
	if db != 0 {
		defer closeDB.Call(db)
	}
	if rc != 0 {
		return nil, fmt.Errorf("Gemini 桌面版登入資料無法開啟")
	}
	const query = `SELECT host_key, name, encrypted_value, (SELECT value FROM meta WHERE key = 'version') FROM cookies WHERE host_key IN ('.google.com', 'google.com', '.gemini.google.com', 'gemini.google.com') LIMIT 80`
	q, _ := syscall.BytePtrFromString(query)
	var stmt uintptr
	rc, _, _ = prepare.Call(db, uintptr(unsafe.Pointer(q)), uintptr(len(query)), uintptr(unsafe.Pointer(&stmt)), 0)
	runtime.KeepAlive(q)
	if stmt != 0 {
		defer finalize.Call(stmt)
	}
	if rc != 0 {
		return nil, fmt.Errorf("Gemini 桌面版登入資料無法查詢")
	}
	var rows []geminiCookie
	for {
		rc, _, _ = step.Call(stmt)
		if rc == 101 || len(rows) >= 80 {
			break
		}
		if rc != 100 {
			return nil, fmt.Errorf("Gemini 桌面版登入資料查詢失敗")
		}
		host := sqliteText(columnText, columnBytes, stmt, 0, 256)
		name := sqliteText(columnText, columnBytes, stmt, 1, 128)
		blob := sqliteBlob(columnBlob, columnBytes, stmt, 2, 8192)
		version, err := strconv.Atoi(sqliteText(columnText, columnBytes, stmt, 3, 16))
		if err != nil {
			return nil, fmt.Errorf("Gemini 桌面版登入資料版本無法讀取")
		}
		if host == "" || name == "" || len(blob) == 0 {
			continue
		}
		rows = append(rows, geminiCookie{host: host, name: name, encrypted: blob, version: version})
	}
	return rows, nil
}

func sqliteText(text, length *syscall.LazyProc, stmt uintptr, col, max int) string {
	buf := sqliteCopy(text, length, stmt, col, max)
	return string(buf)
}

func sqliteBlob(blob, length *syscall.LazyProc, stmt uintptr, col, max int) []byte {
	return sqliteCopy(blob, length, stmt, col, max)
}

func sqliteCopy(column, length *syscall.LazyProc, stmt uintptr, col, max int) []byte {
	n, _, _ := length.Call(stmt, uintptr(col))
	ptr, _, _ := column.Call(stmt, uintptr(col))
	if ptr == 0 || n == 0 || int(n) > max {
		return nil
	}
	buf := make([]byte, int(n))
	move := syscall.NewLazyDLL("kernel32.dll").NewProc("RtlMoveMemory")
	move.Call(uintptr(unsafe.Pointer(&buf[0])), ptr, n)
	runtime.KeepAlive(buf)
	return buf
}

func cryptUnprotect(data []byte) ([]byte, error) {
	type blob struct {
		cbData uint32
		pbData *byte
	}
	var in, out blob
	in.cbData = uint32(len(data))
	in.pbData = &data[0]
	proc := syscall.NewLazyDLL("crypt32.dll").NewProc("CryptUnprotectData")
	free := syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&out)))
	runtime.KeepAlive(data)
	if r == 0 || out.pbData == nil || out.cbData == 0 || out.cbData > 256 {
		return nil, fmt.Errorf("decrypt")
	}
	defer free.Call(uintptr(unsafe.Pointer(out.pbData)))
	buf := make([]byte, out.cbData)
	move := syscall.NewLazyDLL("kernel32.dll").NewProc("RtlMoveMemory")
	move.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(out.pbData)), uintptr(out.cbData))
	return buf, nil
}
