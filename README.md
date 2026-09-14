# aiusage

將 **Claude、Codex、Cursor、Antigravity** 的用量集中在本機面板，以已使用百分比、
剩餘額度與重置時間呈現。支援帳號額度查詢、本機紀錄統計，以及手動設定上限。

以 Go 標準函式庫實作，網頁介面內嵌於單一執行檔，不需要另外安裝前端套件或建立資料庫。
各來源的計量方式不同：**帳號額度百分比與本機 token 統計應分開解讀**。

## 支援來源

| 來源 | 資料取得方式 | 使用條件與限制 |
| --- | --- | --- |
| Claude CLI / Desktop | 讀取 Code 紀錄、Desktop 額度歷史，以及選配的 CLI statusLine 額度快照 | Desktop 必須更新用量頁面才會產生新快照；本機 token 不包含完整 Desktop 聊天用量 |
| Codex CLI | 優先透過已安裝的 `codex app-server` 查詢帳號額度；失敗時使用 session 紀錄中的回報值 | 即時查詢需要已登入、可在 `PATH` 找到的 Codex CLI；本機 token 另由紀錄統計 |
| Cursor IDE / CLI | 使用既有登入資訊，向 Cursor HTTPS 用量介面查詢個人方案額度 | 優先 IDE，再嘗試 CLI；金額以 USD 顯示，無可用登入時可手動填寫 |
| Antigravity CLI / Desktop | 從執行中的本機服務讀取各模型群額度 | 必須開啟並登入；主百分比採使用比例最高的限制，各模型群不相加 |

Windows 提供上述來源的整合。macOS / Linux 可建置主程式，但 **Cursor IDE 登入讀取與
Antigravity 本機服務自動偵測目前僅支援 Windows**；其他平台的 Cursor 請使用 CLI 登入。
Claude Desktop 額度檔探索涵蓋 Windows 與 macOS。

Windows / macOS 會依安裝偵測結果顯示來源，只有殘留紀錄資料夾不一定會出現在面板上。
Linux 目前不做安裝過濾。舊設定中的 `gemini` 會在載入時移除，並補上 `antigravity`；
不沿用 Gemini 的每日限制或紀錄路徑，也不會刪除 Gemini 本身或其紀錄。

各來源的細節：

- [Claude 用量來源與 statusLine 設定](docs/providers/claude.md)
- [Cursor 登入來源、計量與重試規則](docs/providers/cursor.md)
- [Antigravity 模型群額度與本機服務](docs/providers/antigravity.md)

## 發行流程

發行採用 Release PR 作為人工核准點，不直接以手動推送 tag 發布：

1. 功能與修正 PR 以 Conventional Commits 格式合併到 `main`。
2. GitHub Actions 會建立或更新 `chore(main): release ...` PR，集中版本號與變更紀錄。
3. 人工審查並合併 Release PR 後，Actions 才建立 tag 與草稿 GitHub Release。
4. 測試、靜態檢查及六平台建置全部成功後，Actions 上傳產物並公開 Release。

`feat:`、`fix:` 與 breaking change 會依 SemVer 決定下一版；目前在 `0.x` 階段，
`feat:` 採 patch bump。內建版本號由 Release PR 同步更新，不需手動改 tag。
GitHub repository 必須允許 Actions 建立 PR；若要強制不同人核准，請在 `main` 的
branch ruleset 啟用「Require a pull request」與至少一位 approving review。

## 使用 Docker 建置

需要 Docker，並使用 **Linux containers**。主機不需安裝 Go；每次建置透過
`--pull=always golang:latest` 取得最新版官方 Go 映像。專案目前的最低 Go 版本為 1.22，
發行建置使用最新工具鏈。`scripts/release.sh` 會先執行競態測試與靜態檢查，再交叉建置。

在專案根目錄執行（PowerShell）：

```powershell
docker run --rm --pull=always --mount "type=bind,source=$PWD,target=/src" -w /src golang:latest sh scripts/release.sh
```

macOS / Linux shell：

```sh
docker run --rm --pull=always -v "$PWD:/src" -w /src golang:latest sh scripts/release.sh
```

產物位於 `dist/releases/`，原有 `dist/aiusage*.exe` 不會被覆寫：

| 子目錄 | 平台 | 執行檔 |
| --- | --- | --- |
| `windows-amd64/` | Windows x64 | `aiusage.exe`、`aiusage-panel.exe` |
| `windows-arm64/` | Windows ARM64 | `aiusage.exe`、`aiusage-panel.exe` |
| `linux-amd64/` | Linux x64 | `aiusage` |
| `linux-arm64/` | Linux ARM64 | `aiusage` |
| `darwin-amd64/` | Intel MacBook / Mac | `aiusage` |
| `darwin-arm64/` | Apple Silicon MacBook / Mac | `aiusage` |

同一目錄下的 `BUILDINFO.txt` 記錄 Go 版本與建置時間，`SHA256SUMS` 提供八個執行檔的雜湊。
所有發行產物使用 `CGO_ENABLED=0`，不需要 Go runtime；Windows Cursor IDE 登入讀取
會使用系統提供的 `winsqlite3.dll`。

Windows 可雙擊對應架構的 `aiusage-panel.exe`；終端指令使用 `aiusage.exe`：

```powershell
.\dist\releases\windows-amd64\aiusage-panel.exe
```

macOS / Linux 請使用符合架構的產物；例如 Apple Silicon MacBook：

```sh
chmod +x ./dist/releases/darwin-arm64/aiusage
./dist/releases/darwin-arm64/aiusage panel
```

產物是獨立執行檔，尚未封裝為 macOS `.app`，也未簽章或經 Apple 公證；macOS 可能需要在
「系統設定 → 隱私權與安全性」允許開啟。請依來源可信度及系統提示操作。

Docker 用於建置與自動測試，程式應在使用者的主機執行，才能取得該機器的紀錄、登入資訊與
本機服務。跨平台編譯不等於各平台來源整合均已實機驗證，功能限制請見上方支援來源。
若再次建置時目標執行檔正在執行而無法覆寫，先關閉該程式再重試。

## 視窗與指令

Windows 優先以 Edge、其次 Chrome 的 app 模式開啟獨立視窗；macOS 也會嘗試
Edge / Chrome。找不到時改用系統預設瀏覽器，Linux 使用 `xdg-open`。
app 模式使用獨立的 `panel-browser` 設定資料夾；Windows 面板會固定視窗尺寸。
再次啟動時會嘗試重用已在執行的服務，並開啟既有面板網址。

**正常關閉最後一個已連線的面板視窗或分頁後，約 8 秒會自動結束背景程式**。
最小化不會結束程式，重新整理有重新連線的緩衝時間。使用 `-no-browser` 時，
服務會持續執行；主控台版可按 `Ctrl+C` 結束。若視窗未能開啟，可從主控台或
`panel.log` 取得面板網址。

Windows 開機自動啟動：`Win+R` → `shell:startup`，放入 `aiusage-panel.exe` 的捷徑。

以下以 Windows 主控台版為例：

```powershell
.\dist\releases\windows-amd64\aiusage.exe                         # 預設開啟面板
.\dist\releases\windows-amd64\aiusage.exe panel                   # serve 也是同義指令
.\dist\releases\windows-amd64\aiusage.exe panel -port 8765 -no-browser
.\dist\releases\windows-amd64\aiusage.exe report                  # 掃描一次並印出目前用量
.\dist\releases\windows-amd64\aiusage.exe doctor                  # 檢查來源、路徑、欄位與讀取錯誤
.\dist\releases\windows-amd64\aiusage.exe config                  # 印出設定檔路徑
.\dist\releases\windows-amd64\aiusage.exe version
```

| 面板旗標 | 說明 |
| --- | --- |
| `-port N` | 指定本機埠；`0` 為自動挑選，未指定時使用設定檔 |
| `-interval N` | 掃描間隔秒數，正值會覆蓋設定檔的 `refresh_seconds` |
| `-no-browser` | 不自動開啟視窗，並保持服務執行 |

`report` 與 `doctor` 都會執行來源收集，可能觸發與面板相同的額度查詢。
另有 `claude-statusline` 指令供 Claude statusLine 呼叫，從標準輸入接收 JSON，
僅保存其中的 `rate_limits` 額度資料；它不會自動修改 Claude 設定。

## 面板與額度怎麼讀

每列先顯示來源、計量視窗與已使用百分比。按「**詳細資訊 ＋**」可查看用量、剩餘比例、
其他限制、來源時間、錯誤訊息及上限設定。

- **帳號回報值**：直接使用來源回報的百分比或已用／上限。若來源只提供百分比，
  不需要自行猜測 token 上限。Codex、Claude 的「本機累計」只代表可讀的 Code 紀錄，
  不能用來反推帳號的 token 總額度。
- **手動上限**：沒有可用回報值時，以本機用量 ÷ 自訂上限計算。沒有回報值也沒有上限時，
  顯示 `—%`；這不等於已使用 0%。
- **多重限制**：Codex 通常顯示較短視窗，其他視窗放在詳細資訊；Claude 會列出來源提供的
  5 小時、7 天等限制。Antigravity 顯示使用比例最高的模型群限制，其餘分列。
  Cursor 顯示帳單週期，並保留來源提供的 Auto / Composer、API 模型百分比。
- **重置時間**：有官方重置時間時採用回報值。本機滾動視窗的「最舊紀錄滑出」
  只表示一筆紀錄即將離開統計範圍，不表示帳號額度全部重置。
- **警戒與狀態**：低於警戒線使用來源色，達警戒線轉黃，達 100% 轉紅；
  圓點另區分可用、閒置與無資料狀態。速率依最近 15 分鐘的本機事件估算。

本機紀錄預設每 3 秒掃描，帳號額度有各自的更新節奏：

| 來源 | 額度更新節奏 |
| --- | --- |
| Codex | 最快每 30 秒查詢一次；失敗時使用備援，「重新掃描」可立即重試 |
| Claude | 隨掃描讀取本機快照；新資料取決於 Desktop 或 statusLine 是否更新 |
| Cursor | 成功後約 60 秒再查；失敗後 5 分鐘再試，「重新掃描」可立即重試 |
| Antigravity | 約每 60 秒讀取一次；「重新掃描」可立即重試 |

Claude 顯示額度來源與快照樣本時間；Cursor、Antigravity 顯示來源與查詢取得時間。
上述來源超過 45 分鐘沒有更新時會提示資料較舊。
帳號回報值超過 24 小時後不再採用；本機統計或手動設定仍可能顯示。
面板的「更新於」是畫面快照時間，不代表所有來源剛剛完成查詢。

### 設定手動上限

展開詳細資訊，按「設定上限」或「調整上限」，輸入與該列相同單位的數字後儲存。
Cursor 手動模式需同時填入「已用」與「上限」；預設手動單位為次數，若要使用金額，
請將 Cursor 的 `metric` 設為 `usd` 並重啟。Antigravity 不提供手動上限。

**預設 `prefer_reported_limits: true`，可用的帳號回報值會優先於手動數值。**
介面上的「改用手動上限」只會儲存手動數值，不會關閉這個偏好。
若要強制使用手動值，需將此設定改為 `false` 並重啟；它會影響所有來源，
也會讓依賴回報值的 Antigravity 無法顯示額度。一般使用建議保留預設值，
並讓自動取得額度的 Codex / Claude 的 `limit` 保持 `0`。

## 設定檔

首次啟動面板或執行 `report` / `doctor` 時會建立 `config.json`。
實際路徑可用 `aiusage config` 查詢：

- Windows：`%APPDATA%\aiusage\config.json`。
- macOS / Linux：`$XDG_CONFIG_HOME/aiusage/config.json`；未設定時為
  `~/.config/aiusage/config.json`。

**手動修改檔案後需結束並重新啟動程式。**「重新掃描」只會清除讀取位移與記憶體資料、
重新收集來源，不會重新載入設定檔。在面板內儲存上限則會直接更新目前設定並寫回檔案。

以下是 Windows 的有效 JSON 範例。`providers` 僅列 Codex 作示範，載入時會自動補上
缺少的預設來源；要停用某來源，請保留該項並設 `enabled: false`。

```json
{
  "port": 0,
  "refresh_seconds": 3,
  "warn_ratio": 0.8,
  "prefer_reported_limits": true,
  "retention_days": 40,
  "providers": [
    {
      "id": "codex",
      "name": "Codex CLI",
      "enabled": true,
      "roots": [
        "%USERPROFILE%\\.codex\\sessions",
        "%USERPROFILE%\\.codex\\log"
      ],
      "metric": "tokens",
      "window_kind": "rolling",
      "window_seconds": 18000,
      "limit": 0
    }
  ]
}
```

| 設定 | 說明 |
| --- | --- |
| `warn_ratio` | 警戒線比例，預設 `0.8`（80%） |
| `prefer_reported_limits` | 是否優先採用帳號回報值；不控制來源是否連線查詢 |
| `retention_days` | 本機事件的保留／採計天數，預設 40；不刪除原始紀錄檔 |
| `enabled` | 是否收集此來源；停用後若仍偵測到安裝，面板會顯示停用狀態 |
| `roots` | Claude / Codex 的紀錄目錄，可多個，支援 `%USERPROFILE%`、`$HOME` 等環境變數；Cursor / Antigravity 使用專用讀取器 |
| `metric` | 本機計量可用 `tokens` 或 `requests`，Cursor 手動金額可用 `usd`；Antigravity 使用 `quota` |
| `window_kind` | `rolling`、`day`、`week`、`month`；日／週／月依本機時區，週一為一週起點 |
| `window_seconds` | 本機滾動視窗秒數，預設 18000（5 小時）；不會更改官方計量視窗 |
| `limit` | 自訂上限；`0` 表示未設定，不妨礙使用官方回報百分比 |
| `claude_org` | 選用，指定 Claude Desktop 額度快照的組織；未設時使用最新樣本 |

設定資料夾也可能包含 `panel.log`（啟動與錯誤訊息）、`panel.url`（既有面板網址）、
`panel-browser/`（專用瀏覽器設定），以及使用 statusLine 功能後的 `claude-quota.json`。

## 讀不到資料時

先展開該列的詳細資訊，再執行 `aiusage doctor` 查看路徑、命中的用量欄位與讀取錯誤。

- **來源整列不見**：先確認工具的安裝或 CLI 路徑可被偵測；Windows 安裝偵測快取約一分鐘。
- **Codex 即時額度失敗**：確認 CLI 已登入且在 `PATH` 中；有本機 session 時仍可使用備援。
  「重新掃描」可清除輪詢等待並立即重試。
- **Claude 沒有額度或資料過舊**：開啟 Desktop 用量頁面，或依來源文件設定 statusLine。
  只有 Code token 紀錄時，仍可能需要自行設定上限。
- **Cursor 無法查詢**：確認 IDE / CLI 使用帳號登入；API key 模式無法當作方案登入。
  401 / 403 請重新登入；IDE 與 CLI 登入不同帳號時，依面板來源標籤確認顯示哪個帳號。
- **Antigravity 沒有資料**：確認在 Windows 上開啟並登入 CLI 或 Desktop，然後重新掃描。
  本機服務介面若隨版本改變，也可能暫時無法取得額度。
- **本機紀錄未命中**：確認 `roots`。通用掃描只接受 `.json`、`.jsonl`、`.ndjson`，
  每個根目錄最多探索 4,000 個符合條件的檔案，跳過超過 96 MiB 的檔案及敏感檔名。
  `.json` 會在單檔大小上限內完整讀取；無效 JSON 會回報錯誤。
  已讀到檔案仍無用量時，可依 `doctor` 結果檢查 `internal/record` 的欄位映射。

## 隱私與連線

本工具不設中央資料庫，也不將本機對話紀錄上傳至自建服務；**它並非完全離線工具**。

- Claude 整合讀取本機 Code 紀錄與額度快照，不讀 Desktop cookies 或登入憑證。
- Codex 即時額度透過既有 CLI 的帳號介面查詢，可能由 CLI 連線官方服務。
- Cursor 專用讀取器會唯讀取得 IDE / CLI 的 access token，僅用於固定的
  `https://api2.cursor.sh` 用量端點，拒絕 HTTP 轉址，不修改登入資料或另存憑證。
- Antigravity 整合僅查詢本機 loopback 服務；必要時使用該程序提供的 CSRF 驗證值，
  不讀 Google OAuth 憑證，也不直接向雲端送出查詢。
- 通用紀錄掃描會跳過名稱含 `oauth`、`credential`、`auth.json`、`token.json`、
  `secret`、`cookie`、`password`、`.env`、`settings.json` 等字樣的檔案。
  Cursor 的專用登入讀取不屬於這個通用掃描流程。
- 面板只監聽 `127.0.0.1`，頁面與資料 API 需要啟動時產生的隨機 token 或對應 cookie。
  `panel.url` 與 `panel.log` 可能含可存取面板的網址，分享診斷時請排除這些網址。
- `doctor` 顯示路徑、欄位名稱、統計與錯誤，不輸出對話內容；路徑可能包含本機使用者名稱。

若要停止某來源的收集或額度查詢，將該來源設為 `enabled: false` 後重啟。

## 架構與開發

採用 Go 常見的 `cmd` / `internal` 目錄慣例；MVC 的模型、控制器與畫面分開，
來源 IO 與設定儲存經由介面注入，讓統計服務可以獨立測試。

```text
cmd/aiusage/          執行入口
internal/
  app/               指令處理、依賴組裝與生命週期
  model/             設定、事件、額度與快照
  service/           去重、累計增量、統計視窗與設定更新
  source/            codex / claude / cursor / antigravity / logs
  record/            紀錄讀取與解析
  config/            設定檔儲存與遷移
  controller/        HTTP API 與 SSE
  view/              HTML、圖示與終端報表
  desktop/           作業系統、瀏覽器與視窗整合
  buildinfo/         版本資訊
  architecture/      依賴方向測試
docs/                架構及來源文件
scripts/release.sh    測試與跨平台建置
```

詳細的 SOLID 對應、來源擴充契約、依賴圖及已知限制見 [架構說明](docs/architecture.md)。
新增來源透過 `Collector` registry 與 `ProviderPolicy` 接入，service 不依賴具體來源。
設定採深拷貝，只有儲存成功後才更新執行中的設定。

只執行測試及靜態檢查（PowerShell）：

```powershell
docker run --rm --pull=always --mount "type=bind,source=$PWD,target=/src" -w /src golang:latest sh -c "go test -race ./... && go vet ./..."
```

若已有 Go，可在專案根目錄執行 `go test ./...`，或用
`go build -o dist/aiusage ./cmd/aiusage` 建置目前平台。執行入口已移至 `cmd/aiusage`，
原本對根目錄執行 `go build .` 的方式不再適用。

測試涵蓋紀錄與額度解析、累計去重、設定遷移、寫入失敗、並行讀寫、來源重試、
MVC 依賴方向、HTTP API 與面板關閉生命週期。本次重構也修正了 Codex 快取／推理 token
重複計數，以及超過 24 MiB 的 JSON 紀錄被截斷而漏讀的問題。

## 授權

[MIT License](LICENSE)
