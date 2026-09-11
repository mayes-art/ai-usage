# Antigravity CLI / Desktop 額度

取代 Gemini CLI 列。載入舊設定時移除 gemini provider，加入 antigravity 預設設定；
不移用 API key 的每日限制或本機 token。原 Gemini CLI 安裝及個人紀錄不會被刪除。

Windows 自動偵測正在執行的 agy.exe，或安裝路徑屬於 Antigravity 的 language_server
程序及其 loopback 監聽埠。背景 PowerShell 使用隱藏視窗設定，不啟動模型對話。
僅呼叫本機 LanguageServerService/RetrieveUserQuotaSummary；舊服務未實作時退回
GetUserStatus 的 cascadeModelConfigData.clientModelConfigs[].quotaInfo。
不讀 Google OAuth 憑證、不直接傳送到雲端；若 Desktop 提供本機 CSRF 驗證值，
僅用於對應本機服務。拒絕 HTTP 轉址，不使用 HTTP proxy，不停用 TLS 驗證。

remainingFraction 為剩餘比例，顯示已使用百分比為 (1-remainingFraction)*100。
每個模型群的 5h / weekly 額度分開顯示，不將共享的模型額度相加。
主數字選擇使用比例最高的限制，並以模型群與視窗標示；其餘限制列於下方。
CLI 和 Desktop 都開啟時採第一個成功回應的來源，來源會標示在畫面上，不合併帳號。

每分鐘讀取一次，每個本機请求最多 3 秒、整輪最多 20 秒；重新掃描可立即重試。
Antigravity 必須開啟並登入。關閉或錯誤時顯示原因及上次成功資料時間。
目前已在這台 Windows 的 Antigravity CLI 1.2.1 實機驗證；Desktop 自動偵測與
舊介面解析已實作，但本機未找到 Desktop 安裝，故未完成 Desktop 實機驗證。
本機服務介面可能隨版本變動，若未回傳額度，不假造上限或使用量。

官方 CLI 用量說明：https://antigravity.google/docs/cli/commands/usage
