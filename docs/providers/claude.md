# Claude 用量來源

Claude CLI / Desktop 使用同一帳號時共享額度，不把兩者的百分比相加。
面板會查詢 Code 登入帳號的額度，並保留 Desktop 與 statusLine 快照，採樣本時間較新的有效結果。
本機 token 僅代表可讀的 Code 紀錄，不代表完整 Desktop 聊天使用量，也不能反推帳號 token 上限。

主卡固定顯示 **5 小時**用量與該視窗的重置倒數；**7 天**百分比及其他週額度放在展開細節。
5 小時額度缺少或已過期時顯示 `—%`，不會用週額度代替；有效的 0% 正常顯示為 0%。

## 帳號額度查詢

已登入 Claude Code 且有檔案式登入資料時，面板自動以唯讀 GET 查詢
`https://api.anthropic.com/api/oauth/usage`，取得可用的額度百分比與重置時間。
登入資料取自 `CLAUDE_CONFIG_DIR/.credentials.json`；未設定該環境變數時使用
`~/.claude/.credentials.json`。不必讓 CLI 或 Desktop 持續執行，也不必反覆開啟 Desktop 用量頁面。

只支援檔案式 Code 登入，常見於 Windows / Linux；不讀取 macOS Keychain。
只有 Desktop 登入或 Keychain 登入時，仍使用下方的 Desktop / statusLine 快照。
設定 `claude_org` 時，停用這個即時查詢，避免把 Code 登入帳號的其他組織額度帶入；
原有 Desktop 組織選取與 statusLine 等快照讀取維持不變。

更新節奏如下：

- 成功後約 60 秒再查，單次查詢逾時為 5 秒。查詢配合掃描執行，掃描間隔較長或其他來源阻塞時可能延後。
- 一般失敗從 60 秒開始指數退避，最長 15 分鐘；HTTP 429 至少等 5 分鐘。429 / 503 均尊重較長的 `Retry-After`。
- 額度重置到期可提前重查，但不繞過失敗退避。面板的「重新掃描」可立即重試。
- 失敗時保留上次成功的帳號快照，與可用的 Desktop / statusLine 快照比較時間；缺少資料不當成已用 0%。

這是 Claude Code 使用的內部用量介面，沒有公開穩定 API 契約；服務改動或限流可能影響可用性。
HTTP 401 / 403 或登入過期時，請先在 Claude Code 重新登入，再按「重新掃描」。
可使用 `claude auth login` 並選擇與 Desktop 相同的帳號／組織。Desktop 的登入與用量頁面
不會替 aiusage 更新 Code 登入；Desktop 歷史檔也不保證每次用量頁重整都立即寫入。
沒有有效主額度且登入需要處理時，主卡顯示「需登入」，詳細資訊顯示原因。
登入錯誤後偵測到登入檔路徑、修改時間或大小變更，會略過登入錯誤等待並重試；
一般連線失敗及 429 / 503 的退避不受此偵測影響。
aiusage 不會刷新 token、改寫登入檔或另外保存 token；憑證僅在記憶體中用於上述固定 HTTPS 端點，拒絕轉址。

## Desktop 快照備援

Windows 的即時查詢亦可使用下節說明的 WSL 登入；Desktop 快照仍只從主機讀取。

自動發現一般安裝的 `%APPDATA%\Claude`、Microsoft Store 版
`%USERPROFILE%\AppData\Local\Packages\Claude_*\LocalCache\Roaming\Claude`，以及 macOS 的
`~/Library/Application Support/Claude`。
Desktop 整合僅讀取 `plan-usage-history.json`（version 1 / 2），不讀取 Desktop 登入憑證或 cookies。
`fh`、`sd` 是 Desktop 自身程式所定義的 5 小時與 7 天百分比。
讀取最新時間戳記的樣本；預設使用最近寫入的組織，可在 claude provider 設定
`claude_org` 固定組織。

Desktop 必須啟動並更新用量頁面，快照檔才會更新。檔案沒有重置時間，因此不推算重置時間。
面板顯示實際樣本或帳號查詢取得時間；超過 45 分鐘提示資料較舊，超過 24 小時不再用於計算。

## CLI / Desktop Code 紀錄與 statusLine

保留 `.claude/projects` 紀錄讀取，支援 `CLAUDE_CONFIG_DIR/projects` 與 Desktop
`claude-code-sessions`。CLI 不在 PATH 並不影響已存在的紀錄讀取。

Claude CLI 的官方 statusLine 可提供 `rate_limits` 欄位，包括額度百分比與重置時間。
欄位是否出現取決於登入方案與該次 session 是否已有 API 回應；每個視窗都可能缺少。
可將自己的 statusLine 接到 `aiusage.exe claude-statusline`，從標準輸入接收 JSON 並保存額度快照。
例如在沒有自訂 statusLine 時，於 `.claude/settings.json` 合併：

```json
{"statusLine":{"type":"command","command":"C:/path/to/aiusage.exe claude-statusline"}}
```

請將範例路徑換成主控台版執行檔的實際位置。若已經有 statusLine，請整合既有命令，
避免覆蓋原本功能。程式不會自動改動 Claude 設定。
此命令僅保存 `rate_limits` 到 aiusage 設定目錄的 `claude-quota.json`，不保存對話內容或 `context_window` 百分比。
statusLine 更新並提供額度欄位時才有新樣本；單純縮短面板掃描間隔不會讓舊快照變新。

欄位與執行時機見 [Claude Code statusLine 官方說明](https://code.claude.com/docs/en/statusline)。
內部用量端點的限制與使用情境可參考官方專案中的
[429 回報 #30930](https://github.com/anthropics/claude-code/issues/30930) 與
[唯讀用量權限提案 #81015](https://github.com/anthropics/claude-code/issues/81015)；issue 內容不代表公開 API 的穩定性保證。

## Windows / WSL 帳號來源

- Windows 主機登入可用時直接使用主機，不查詢 WSL 帳號。
- 主機缺少有效登入、過期、缺少權限或 HTTP 401 / 403 時，以 `wsl.exe --list --running --quiet`
  探索執行中的發行版，排除 Docker Desktop 內部發行版。依名稱排序，最多檢查 8 個，使用第一個可查詢的登入。
- 每個發行版以預設 Linux 使用者執行固定非互動 `sh` 指令，讀取
  `${CLAUDE_CONFIG_DIR:-$HOME/.claude}/.credentials.json`。不讀 shell profile；只在互動 shell 設定的自訂路徑可能不可見。
  不探索其他 Linux 使用者、不複製登入檔至 Windows，也不刷新 token。
- WSL 列舉及個別讀取各最多 2 秒，與 HTTP 共用一次查詢的 5 秒總預算；輸出及登入資料有大小限制。
  使用 Windows 端的固定 HTTPS 用量端點。網路錯誤、429 / 503 不切換帳號重試，繼續遵守原有退避。
- 來源標籤會標示主機或 WSL 發行版。不合併不同環境帳號額度；選中 WSL 後，主機 Desktop / statusLine
  快照不會覆蓋該 WSL 帳號結果。後續主機登入恢復可用時會恢復主機優先。
- 主機登入檔更新的立即重試偵測仍只監看主機；WSL 重新登入或剛啟動後可按「重新掃描」立即重試。
  平常依成功每分鐘、失敗退避的節奏查詢。不會為探索而主動啟動已停止的發行版。
- 此次 WSL 支援限定帳號額度，本機 token 紀錄仍沿用主機掃描路徑。僅在 WSL 安裝 Claude 時，
  取得有效回報後也會顯示 Claude 卡片，不受主機安裝偵測隱藏。

WSL 指令依據：[Microsoft WSL 指令參考](https://learn.microsoft.com/en-us/windows/wsl/basic-commands)。

## 建置與測試

請依 [開發文件的 Docker 建置步驟](../development.md#使用-docker-建置)產生各平台執行檔。
實作位於 `internal/source/claude`，CLI 額度解析位於 `internal/record`。
