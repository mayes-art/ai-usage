# Claude 用量來源

Claude CLI / Desktop 顯示同一帳號的共享額度，不把兩個來源的百分比相加。

## Desktop

自動發現一般安裝的 `%APPDATA%\Claude`，以及 Microsoft Store 版
`%USERPROFILE%\AppData\Local\Packages\Claude_*\LocalCache\Roaming\Claude`。
僅讀取 `plan-usage-history.json`（version 1 / 2），不讀取登入憑證或 cookies。
`fh`、`sd` 是 Desktop 自身程式所定義的 5 小時與 7 天百分比。
讀取最新時間戳記的樣本；預設使用最近寫入的組織，可在 claude provider 設定
`claude_org` 固定組織。這是本機歷史快照，不是小工具直接查詢即時 API。

Desktop 必須啟動並更新用量，檔案才會更新。面板標示實際樣本時間；超過 45 分鐘
提示資料較舊，超過 24 小時不再用於計算。檔案沒有重置時間，因此不推算重置時間。

## CLI / Desktop Code

保留 `.claude/projects` 紀錄讀取，支援 `CLAUDE_CONFIG_DIR/projects` 與 Desktop
`claude-code-sessions`。CLI 不在 PATH 並不影響已存在的紀錄讀取。
token 僅代表可讀的本機 Code 紀錄，不代表完整 Desktop 聊天使用量。

Claude CLI 支援官方 statusLine 的 `rate_limits` 欄位。可將自己的 statusLine
接到 `aiusage.exe claude-statusline`，讓它從標準輸入接收 JSON 並保存額度快照。
例如在沒有自訂 statusLine 時，於 `.claude/settings.json` 合併：

```json
{"statusLine":{"type":"command","command":"C:/path/to/aiusage.exe claude-statusline"}}
```

請將範例路徑換成主控台版執行檔的實際位置。若已經有 statusLine，請整合既有命令，
避免覆蓋原本功能。程式不會自動改動 Claude 設定。
此命令僅保存 rate_limits，不保存對話內容或 context_window 百分比。
CLI 與 Desktop 同時有額度時，使用樣本時間較新的來源。

官方欄位說明：https://code.claude.com/docs/en/statusline

## 建置

請依 [README 的 Docker 建置步驟](../../README.md#使用-docker-建置)產生各平台執行檔。
實作位於 `internal/source/claude`，CLI 額度解析位於 `internal/record`。
