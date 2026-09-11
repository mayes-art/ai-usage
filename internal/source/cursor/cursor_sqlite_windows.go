package cursor

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// Windows 11 ships SQLite. Open read-only so the IDE's database and WAL stay intact.
func readCursorIDEToken(path string) (string, error) {
	dll := syscall.NewLazyDLL("winsqlite3.dll")
	if err := dll.Load(); err != nil {
		return "", fmt.Errorf("Windows SQLite 無法載入")
	}
	open := dll.NewProc("sqlite3_open_v2")
	closeDB := dll.NewProc("sqlite3_close")
	prepare := dll.NewProc("sqlite3_prepare_v2")
	step := dll.NewProc("sqlite3_step")
	finalize := dll.NewProc("sqlite3_finalize")
	column := dll.NewProc("sqlite3_column_text")
	length := dll.NewProc("sqlite3_column_bytes")
	for _, proc := range []*syscall.LazyProc{open, closeDB, prepare, step, finalize, column, length} {
		if err := proc.Find(); err != nil {
			return "", fmt.Errorf("Windows SQLite 缺少必要介面")
		}
	}
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return "", err
	}
	var db uintptr
	rc, _, _ := open.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&db)), 1, 0)
	runtime.KeepAlive(p)
	if db != 0 {
		defer closeDB.Call(db)
	}
	if rc != 0 {
		return "", fmt.Errorf("Cursor IDE 登入資料無法唯讀開啟 (SQLite %d)", rc)
	}
	query, _ := syscall.BytePtrFromString("SELECT value FROM ItemTable WHERE key='cursorAuth/accessToken' LIMIT 1")
	var stmt uintptr
	rc, _, _ = prepare.Call(db, uintptr(unsafe.Pointer(query)), uintptr(len("SELECT value FROM ItemTable WHERE key='cursorAuth/accessToken' LIMIT 1")), uintptr(unsafe.Pointer(&stmt)), 0)
	runtime.KeepAlive(query)
	if stmt != 0 {
		defer finalize.Call(stmt)
	}
	if rc != 0 {
		return "", fmt.Errorf("Cursor IDE 登入資料暫時無法讀取 (SQLite %d)", rc)
	}
	rc, _, _ = step.Call(stmt)
	if rc != 100 {
		return "", fmt.Errorf("Cursor IDE 尚未登入或資料庫忙碌")
	}
	n, _, _ := length.Call(stmt, 0)
	ptr, _, _ := column.Call(stmt, 0)
	if ptr == 0 || n == 0 || n > 16384 {
		return "", fmt.Errorf("Cursor IDE 登入資料無效")
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(n))), nil
}
