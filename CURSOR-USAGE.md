# Cursor IDE / CLI

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

金額原始單位為美分，面板轉為 USD；方案百分比按照 IDE 邏輯使用 includedSpend / limit。
不把 bonusSpend、totalSpend 或團隊 pooledUsed 當作個人方案已使用金額。
Auto / Composer 與 API 模型百分比各自顯示，重置時間使用帳單週期終點。
只有百分比、沒有金額上限時不顯示虛構的金額。

本機 API 整合依據已安裝 IDE 的實際介面，非承諾穩定的公開個人 usage API；
若 Cursor 改版介面，面板會顯示錯誤。沒有可用登入時保留手動填入的退路。
原本廣泛掃描 `.cursor` 不會讀到此帳號額度，目前 Cursor 改採這個專用讀取器。
