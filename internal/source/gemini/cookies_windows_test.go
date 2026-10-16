package gemini

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestReadGeminiCookiesSynthetic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Cookies")
	dll := syscall.NewLazyDLL("winsqlite3.dll")
	p, _ := syscall.BytePtrFromString(path)
	var db uintptr
	rc, _, _ := dll.NewProc("sqlite3_open_v2").Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&db)), 6, 0)
	runtime.KeepAlive(p)
	if rc != 0 {
		t.Fatal("cannot create synthetic cookie database")
	}
	query, _ := syscall.BytePtrFromString(`CREATE TABLE meta (key TEXT, value TEXT);
INSERT INTO meta VALUES ('version', '24');
CREATE TABLE cookies (host_key TEXT, name TEXT, encrypted_value BLOB);
INSERT INTO cookies VALUES ('.google.com','SID',X'763130010203');
INSERT INTO cookies VALUES ('accounts.google.com','SID',X'763130010203');`)
	rc, _, _ = dll.NewProc("sqlite3_exec").Call(db, uintptr(unsafe.Pointer(query)), 0, 0, 0)
	runtime.KeepAlive(query)
	dll.NewProc("sqlite3_close").Call(db)
	if rc != 0 {
		t.Fatal("cannot initialize synthetic cookie database")
	}
	rows, err := readGeminiCookies(path)
	if err != nil || len(rows) != 1 || rows[0].version != 24 || rows[0].host != ".google.com" || rows[0].name != "SID" || len(rows[0].encrypted) != 6 {
		t.Fatalf("cookie metadata read failed: count=%d, err=%v", len(rows), err)
	}
}

func TestGeminiReadSharedWithWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Local State")
	if err := os.WriteFile(path, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ := syscall.UTF16PtrFromString(path)
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(h)
	data, err := readShared(path)
	if err != nil || string(data) != "synthetic" {
		t.Fatalf("shared read failed: %v", err)
	}
}
