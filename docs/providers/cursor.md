# Cursor IDE / CLI

來源實作位於 `internal/source/cursor`；建置方式見 [README](../../README.md#使用-docker-建置)。

Windows 11 版依序使用 Cursor IDE 與 CLI 的既有登入資訊，呼叫目前安裝版本 IDE
使用的唯讀 `aiserver.v1.DashboardService/GetCurrentPeriodUsage` 介面。
IDE 使用 Windows 內建 SQLite 唯讀開啟 `Cursor/User/globalStorage/state.vscdb`，
僅查詢 `cursorAuth/accessToken`；CLI 使用 `%APPDATA%/Cursor/auth.json` 的 accessToken。
不修改登入資料、不更新 refresh token、不記錄或另存憑證。只送至固定 HTTPS Cursor
官方主機，拒絕轉址。CLI 的 API key 模式不當作可用的方案登入。

預設優先 IDE，找不到有效登入或查詢失敗時嘗試 CLI；來源顯示在面板上。
這是帳號總額度，IDE / CLI 同帳號的用量不相加。若兩者登入不同帳號，顯示成功來源
對應的帳號額度，請以來源標籤確認。

每 60 秒查詢，12 秒總逾時；失敗後 5 分鐘再試，也可按重新掃描立即重試。
失敗時顯示原因並暫留上次成功資料與時間，45 分鐘後標示過舊，24 小時後停止使用。
401/403 時請在 IDE 或 CLI 重新登入。整個讀取流程不啟動主控台子程序。

金額原始單位為美分，面板轉為 USD。方案百分比一律採用 `totalPercentUsed`，
已用金額由 `totalPercentUsed × limit` 換算，`limit` 為方案上限。
`includedSpend`、`bonusSpend`、`totalSpend` 都是**可用額度**而非已使用金額
（實機回傳 `includedSpend + bonusSpend = totalSpend`）；用 `includedSpend / limit`
算百分比會固定得到 100%，不可當作用量。團隊 pooledUsed 同樣不是個人方案已使用金額。
沒有 `totalPercentUsed` 時方案總量視為未知，只要有可用分項仍會顯示分項；超過 100% 不截斷。
面板在 Cursor 項目下直接顯示 **Cursor model** 與 **Other model** 兩個子項目，各有獨立百分比與進度條；
不需展開詳細資訊。原本方案總百分比改放在詳細資訊，不再用單一 100% 代表所有模型池。
有各池金額時另顯示已用／上限 USD，重置時間使用帳單週期終點。
只有百分比、沒有金額上限時不顯示虛構的金額。

欄位對應已核對本機 Cursor 3.17.21 的用量介面：`autoPercentUsed` / `autoSpend` / `autoLimit`
為 Cursor model，`apiPercentUsed` / `apiSpend` / `apiLimit` 為 Other model。
優先使用各池回報百分比，缺少時才以該池明確提供的已用／上限計算；不使用方案總上限推算分項，
也不把兩池加總或平均。即使來源沒有總額，只要有可用分項也能顯示。
此分類與 [Cursor 官方兩個用量池說明](https://cursor.com/help/models-and-usage/usage-limits) 一致。

缺少分項資料顯示 `—%`，明確的零才顯示 `0%`；無登入、停用、手動總額或過期資料不會假造分項。
分項資料與來源共用原有快取及過期規則，45 分鐘後在子項目下提示資料較舊。
Windows/macOS 的面板視窗會為兩個子項目增加高度；未安裝 Cursor 時不增加來源項目。

本機 API 整合依據已安裝 IDE 的實際介面，非承諾穩定的公開個人 usage API；
若 Cursor 改版介面，面板會顯示錯誤。沒有可用登入時保留手動填入的退路。
原本廣泛掃描 `.cursor` 不會讀到此帳號額度，目前 Cursor 改採這個專用讀取器。
