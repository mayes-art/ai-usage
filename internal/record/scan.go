package record

import (
	"aiusage/internal/config"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxFileSize    = 96 << 20 // 單一檔案上限，超過就跳過
	maxFilesPerDir = 4000
	readChunkCap   = 24 << 20 // 單次 tick 每個檔案最多讀進來的量
)

// 絕不讀取的檔名片段：憑證、金鑰、設定。
// 這個工具只需要 token 計數，沒有理由碰到這些檔案。
var blockedNameParts = []string{
	"oauth", "credential", "token.json", "tokens.json", "keys", "keyring",
	"secret", "auth.json", "cookie", "password", ".env", "settings.json",
	"mcp.json", "installation", "id_rsa",
}

func blockedFile(name string) bool {
	l := strings.ToLower(name)
	for _, b := range blockedNameParts {
		if strings.Contains(l, b) {
			return true
		}
	}
	return false
}

type FileState struct {
	offset  int64
	size    int64
	modUnix int64
}

// Discover 列出一個來源底下所有值得讀的檔案。
func Discover(roots []string) (files []string, hitRoots []string, errs []string) {
	seen := map[string]bool{}
	for _, r := range roots {
		root := config.ExpandVars(r)
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		hitRoots = append(hitRoots, root)
		count := 0
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 個別子目錄讀不到就跳過，不讓整次掃描失敗
			}
			if d.IsDir() {
				n := strings.ToLower(d.Name())
				if n == "node_modules" || n == ".git" || n == "cache" {
					return filepath.SkipDir
				}
				return nil
			}
			if count >= maxFilesPerDir {
				return filepath.SkipAll
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".jsonl" && ext != ".json" && ext != ".ndjson" {
				return nil
			}
			if blockedFile(d.Name()) {
				return nil
			}
			if fi, err := d.Info(); err == nil && fi.Size() > maxFileSize {
				return nil
			}
			if !seen[p] {
				seen[p] = true
				files = append(files, p)
				count++
			}
			return nil
		})
		if err != nil {
			errs = append(errs, root+": "+err.Error())
		}
	}
	return files, hitRoots, errs
}

// ReadNew 從上次的位置繼續讀一個檔案，回傳新增的記錄。
// JSONL：逐行、以位移續讀。
// 單一 JSON 檔（例如 logs.json 是個陣列）：檔案變動就整份重讀，靠內容雜湊去重。
func ReadNew(path string, st *FileState, cutoff time.Time) (records [][]byte, mtime time.Time, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	mtime = fi.ModTime()
	size := fi.Size()
	if size > maxFileSize {
		return nil, mtime, fmt.Errorf("file exceeds %d MiB limit", maxFileSize>>20)
	}

	// 檔案太舊，連開都不用開。
	if mtime.Before(cutoff) {
		st.size, st.modUnix, st.offset = size, mtime.Unix(), size
		return nil, mtime, nil
	}
	// 沒變動就不做事。
	if st.size == size && st.modUnix == mtime.Unix() && st.offset >= size {
		return nil, mtime, nil
	}
	// 檔案被截斷或輪替，從頭讀。
	if size < st.offset {
		st.offset = 0
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, mtime, err
	}
	defer f.Close()

	isJSONL := strings.ToLower(filepath.Ext(path)) != ".json"
	if !isJSONL {
		// A JSON document cannot be resumed in the middle. Read the complete
		// bounded document, including files larger than the JSONL chunk size.
		data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
		if err != nil {
			return nil, mtime, err
		}
		if len(data) > maxFileSize {
			return nil, mtime, fmt.Errorf("file exceeds %d MiB limit", maxFileSize>>20)
		}
		records, err := splitJSONDoc(data)
		if err != nil {
			return nil, mtime, err
		}
		st.size, st.modUnix, st.offset = size, mtime.Unix(), size
		return records, mtime, nil
	}

	if _, err := f.Seek(st.offset, io.SeekStart); err != nil {
		return nil, mtime, err
	}
	data, err := io.ReadAll(io.LimitReader(f, readChunkCap))
	if err != nil {
		return nil, mtime, err
	}
	if isBinary(data) {
		st.offset, st.size, st.modUnix = size, size, mtime.Unix()
		return nil, mtime, nil
	}
	// 只處理完整的行；殘缺的尾巴留到下一輪。
	lastNL := bytes.LastIndexByte(data, '\n')
	if lastNL < 0 {
		return nil, mtime, nil
	}
	complete := data[:lastNL]
	st.offset += int64(lastNL) + 1
	st.size, st.modUnix = size, mtime.Unix()

	for _, line := range bytes.Split(complete, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) < 8 || line[0] != '{' {
			continue
		}
		records = append(records, line)
	}
	return records, mtime, nil
}

// splitJSONDoc 把一份 JSON 文件拆成多筆記錄：
// 頂層是陣列就逐一取出，否則整份當成一筆。
func splitJSONDoc(data []byte) ([][]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records [][]byte
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				return records, nil
			}
			return nil, fmt.Errorf("invalid JSON document: %w", err)
		}
		if len(raw) > 0 && raw[0] == '[' {
			var items []json.RawMessage
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, err
			}
			for _, item := range items {
				records = append(records, item)
			}
		} else if len(raw) > 0 && raw[0] == '{' {
			records = append(records, raw)
		}
	}
}

func isBinary(data []byte) bool {
	n := len(data)
	if n > 512 {
		n = 512
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

func hashBytes(b []byte) string {
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:8])
}
