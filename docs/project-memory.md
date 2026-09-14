# aiusage 專案記憶

最後核對：2026-09-14；程式基準：`6133495`（MVC 分層與跨平台發行）。
後續工作目錄變更：Cursor 雙模型池顯示，詳見下方計量規則與來源文件。
此文件供後續需求快速接續；環境與程式有變動時重新核對相關部分。

## 產品與使用方式

aiusage 把 Claude、Codex、Cursor、Antigravity 的用量放在本機面板，顯示已使用比例、剩餘量、重置時間、來源狀態及詳細資訊，也提供終端報表與診斷。

- Go module：`aiusage`，最低 Go 版本 1.22；無第三方 Go 套件。
- 使用者偏好：程式碼不要放太多註解；必要說明精簡，其餘寫入文件。
- 介面為繁體中文；`internal/view/ui.html` 包含 HTML、CSS 與原生 JavaScript，由 `go:embed` 內嵌。沒有 npm 專案或獨立前端伺服器。
- 單一執行檔、本機 HTTP 服務、記憶體統計與 JSON 設定檔；沒有應用程式資料庫。Cursor 的 SQLite 讀取是既有登入資料的專用整合。
- 無參數、`panel`、`serve` 都啟動面板；另有 `report`、`doctor`、`config`、`version`、`claude-statusline`。
- 面板旗標：`-port`、`-interval`、`-no-browser`。`report` / `doctor` 也會收集來源，可能觸發帳號查詢。

## 需求對應位置

| 使用者想調整的內容 | 優先閱讀／修改位置 |
| --- | --- |
| 版面、顏色、文字、展開細節、上限表單 | `internal/view/ui.html` |
| 終端報表與診斷文字 | `internal/view/console.go` |
| 已用比例、剩餘量、時間視窗、重置時間、狀態 | `internal/service/snapshot.go`、`internal/model/usage.go`、`internal/model/snapshot.go` |
| 去重、累計增量、掃描、重掃、保留期 | `internal/service/store.go` |
| 紀錄讀不到、欄位改名、token 重複計數 | `internal/record/scan.go`、`internal/record/extract.go`、`internal/record/claude.go`、`internal/source/logs/collector.go` |
| 某一家來源的額度或重試 | `internal/source/<來源>/collector.go` 與同資料夾實作 |
| Cursor model / Other model 子項目 | `cursor.go` 的 `cursorUsageGroup` → `Reported.Groups` → `ProviderSnapshot.UsageGroups` → `ui.html` 的 `buildUsageGroups` |
| HTTP API、儲存上限、SSE、最後視窗關閉 | `internal/controller/server.go` |
| CLI 指令、啟動順序、掃描排程、來源註冊 | `internal/app/app.go`；入口 `cmd/aiusage/main.go` |
| 預設設定、舊設定遷移、設定路徑 | `internal/config/config.go`、`internal/model/config.go` |
| 安裝偵測、瀏覽器模式、視窗大小、既有服務重用 | `internal/desktop/`，注意檔名與 build tag 的平台限制 |
| 版本與發行產物 | `internal/buildinfo/version.go`、`scripts/release.sh` |
| 新增供應商 | 新增 `internal/source/<來源>`，更新 `app.newStore` registry、model ID、config 預設及 desktop 偵測；按需補 UI |

## 資料流與架構契約

`cmd/aiusage → app.Run → config + collectors + service.Store → controller/view`。

1. `app` 載入設定、注入來源與設定儲存實作，首次 `ScanOnce` 後啟動面板，再依間隔收集。
2. Collector 處理來源 IO、檔案位移與查詢快取，交付 `model.Collection`（增量事件、最新額度、診斷）。
3. Store 以事件鍵去重，以來源及 session 計算累計差額，保留有效事件與最新額度。
4. `Snapshot` 將資料套用計量視窗、手動／回報優先權、安裝過濾與 `ProviderPolicy`，組成前端資料。
5. 前端首次讀快照，再以 SSE 更新；編輯上限時暫停 SSE 重繪，避免打斷輸入。串流錯誤後每 3 秒嘗試重連。

核心介面：`service.Collector`（`Collect`、`Reset`）、`service.ConfigRepository`（`Save`）、`controller.UsageService`。來源可選配 `Policy()`。

- Store 的 `scanMu` 序列化收集與重設，`mu` 保護資料；快照可與收集並行。
- Collector 不可在交付後原地修改 Collection；`Policy()` 可並行呼叫，回傳不可變描述。
- Config 使用深拷貝；更新先成功儲存才替換執行中設定，失敗保留原值。
- `internal/architecture/dependencies_test.go` 約束依賴：model 僅標準庫，service 僅 model，controller 僅 model/view，view 僅 model。

## 四種來源

| 來源 | 現有資料路徑與主要行為 | 更新／平台限制 |
| --- | --- | --- |
| Claude | Code 紀錄 + Desktop `plan-usage-history.json` + 選配 `claude-quota.json`，選最新可用快照；可用 `claude_org` 選組織 | 每次掃描讀取；Desktop 用量頁或 statusLine 必須產生新樣本。Desktop 路徑探索涵蓋 Windows/macOS |
| Codex | `codex app-server` 帳號額度與 session 紀錄備援，本機 token 另外統計；額度選時間較新的結果 | 最快 30 秒一次，即時查詢逾時 8 秒；需要 PATH 可找到且已登入的 CLI |
| Cursor | 唯讀使用 IDE、其次 CLI 登入，查固定 `api2.cursor.sh` HTTPS 用量端點；方案金額由 cents 換成 USD，不合併帳號或團隊支出 | 成功 60 秒，失敗 5 分鐘；IDE SQLite 登入讀取僅 Windows，其他平台使用 CLI |
| Antigravity | 查執行中的本機服務，依模型群顯示配額；主列採最高使用比例，不把群組相加 | 約 60 秒；服務自動偵測僅 Windows，需開啟並登入，無手動上限 |

「重新掃描」會重設來源快取及檔案位移，再收集，因此也能立即重試帳號查詢。
Windows/macOS 依安裝偵測顯示來源；Linux 不過濾。Windows 安裝偵測快取約一分鐘。

## 不可混淆的計量規則

- `Reported.PercentUsed` 是 0–100 的百分比；`ProviderSnapshot.Percent` / `LimitWindow.Percent` 是比例（0.8 代表 80%，可超過 1）。
- 帳號額度與本機 token 不等價；只有百分比時不能反推總 token 上限。未知應顯示 `—%`，不能當成 0%。
- `prefer_reported_limits: true` 時，有效回報優先於手動值；關閉此全域偏好不會停止查詢，而且 Antigravity 將無額度可顯示。
- 回報超過 24 小時不採用。帶來源標記的快照超過 45 分鐘顯示資料較舊；畫面更新時間不是來源樣本時間。
- 目前 UI 的「改用手動上限」只存手動數值，沒有切換回報偏好。此為已知介面語意限制，不能當成切換已完成。
- 本機預設 5 小時滾動視窗；日、週、月以本機時區切分，週一為週起點。
- 回報重置時間優先；「最舊紀錄滑出」只代表一筆本機事件離開統計視窗。
- 速率依視窗內最近 15 分鐘事件估算；Codex 快取與推理 token 是分項子集，修改解析時不可重複累加。
- Cursor 手動值需「已用」及「上限」，預設單位是次數；金額手動模式需設定 `metric: usd` 並重啟。
- Cursor 主列直接顯示 Cursor model / Other model 兩池；方案總百分比留在詳細資訊。`auto*` 對應 Cursor model，`api*` 對應 Other model，依本機 Cursor 3.17.21 核對。
- Cursor 方案總量只用回報的 `totalPercentUsed`，已用金額由百分比乘上 `limit` 換算。`includedSpend`、`bonusSpend`、`totalSpend` 是可用額度而非已用金額（`includedSpend + bonusSpend = totalSpend`），拿 `includedSpend / limit` 當百分比會固定顯示 100%；此為 2026-09-14 修正的實機錯誤。
- `Reported.Groups` 的百分比為 0–100，`UsageGroups` 為比例；nil 表示未知。各池先用回報百分比，否則用該池已用／上限，不加總、不平均，也不借用方案總上限。僅有分項的回報亦可用。
- 分項沿用回報偏好、停用、45 分鐘較舊警示與 24 小時過期規則。缺資料顯示 `—%`；額外視窗高度由 `desktop.panelHeight` 統一計算。

## HTTP 與桌面生命週期

| 路徑 | 用途 |
| --- | --- |
| `/` | 內嵌面板頁面 |
| `/api/snapshot` | 目前快照 |
| `/api/stream` | SSE 快照與面板連線追蹤 |
| `/api/limit` | POST `{provider, limit, used?, window_hours?}`，更新並儲存設定 |
| `/api/rescan` | POST，背景重設及掃描；回應成功只表示已啟動工作 |
| `/api/doctor` | 診斷結果 |

頁面與資料 API 需要本機連線及 URL `t` token 或 `aiusage` cookie；圖示可匿名取得。服務僅綁 `127.0.0.1`。
Windows 優先 Edge、其次 Chrome app 模式；macOS 也嘗試 Edge/Chrome，否則使用預設瀏覽器。瀏覽器採獨立 `panel-browser` profile。
最後一個已連線面板的 SSE 中斷約 8 秒後結束程式；最小化不結束，重新整理有緩衝。`-no-browser` 不啟用自動退出。
既有服務透過 `panel.url` 探測與重用；執行生命週期由 `app.runPanel` 管理。

## 設定、掃描與資料界線

- 預設：隨機埠、每 3 秒掃描、80% 警戒、優先回報、保留 40 天。
- 設定檔：Windows `%APPDATA%/aiusage/config.json`；其他平台 `$XDG_CONFIG_HOME/aiusage/config.json`，否則 `~/.config/aiusage/config.json`。
- 磁碟手改設定需重啟；重掃不重新載入設定。面板儲存則立即生效。
- 缺少的預設來源會補回；要停用請保留項目並設 `enabled: false`。舊 `gemini` 項目移除並補 Antigravity，不搬用其上限或紀錄路徑。
- 掃描 `.json` / `.jsonl` / `.ndjson`；每根目錄最多 4,000 個符合條件檔案、單檔最多 96 MiB。JSON 完整讀取；JSONL 每輪最多 24 MiB，只處理完整換行。
- 通用掃描排除敏感檔名；保留期僅影響記憶體採計，不刪原始紀錄。
- Cursor 憑證限專用讀取器唯讀使用，拒絕轉址；Claude 不讀 Desktop cookies；Antigravity 查 loopback 服務。
- `panel.url` / `panel.log` 可能含可存取面板的網址，不能放入共享記憶或診斷附件。

## 開發與驗證

在專案根目錄，有 Go 時：

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o dist/aiusage ./cmd/aiusage
```

Windows 若需競態檢查但沒有相應 C 工具鏈，可用 Linux Docker。PowerShell 唯讀掛載的驗證方式：

```powershell
docker run --rm --mount "type=bind,source=$PWD,target=/src,readonly" -w /src golang:latest sh -c "go test -race ./... && go vet ./..."
```

發行需可寫掛載：

```powershell
docker run --rm --pull=always --mount "type=bind,source=$PWD,target=/src" -w /src golang:latest sh scripts/release.sh
```

發行腳本先競態測試及 vet，再以 `CGO_ENABLED=0` 建置 Windows/Linux/macOS 的 amd64/arm64；Windows 額外產出 GUI 版，共八個執行檔。產物在 `dist/releases/`，含 `BUILDINFO.txt`、`SHA256SUMS`；`dist/` 被 Git 忽略。版本變數目前為 `0.1.0`。
跨平台編譯不代表來源整合已實機驗證；沒有 macOS `.app`、簽章或公證流程。

## 已知事項與文件索引

既有資料邊界：缺 session ID 的累計紀錄可能共用累計鍵；同一秒、大小不變的檔案覆寫可能漏判。此次檢視另外記錄了上限表單錯誤處理與 API 驗證問題，詳見檢視紀錄。這些都是待改善事項，不是產品要求。

- [README](../README.md)：使用、建置、設定與疑難排解。
- [架構說明](architecture.md)：分層與擴充契約。
- [Claude](providers/claude.md)、[Cursor](providers/cursor.md)、[Antigravity](providers/antigravity.md)：來源細節；Codex 實作從 `internal/source/codex/` 閱讀。
- [2026-09-14 檢視紀錄](review-2026-09-14.md)：此次發現及驗證範圍。
- [Cursor 雙模型池 QA](qa-cursor-model-pools.md)：自動測試、畫面及建置結果，以及登入後完成的 Claude CLI QA 與修正紀錄。

後續需求可直接說「調整面板顏色」、「修正 Cursor 額度更新」或「新增來源」，由此文件定位程式。完成相關變更時更新受影響章節及核對日期，移除已不成立的限制；不要累積聊天逐字稿或把未確認的推測寫成事實。
