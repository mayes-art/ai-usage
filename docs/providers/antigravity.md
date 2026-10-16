# Antigravity CLI / Desktop 額度

來源實作位於 `internal/source/antigravity`；自動偵測目前僅支援 Windows。

取代 Gemini CLI 列。載入舊設定時移除 gemini provider，加入 antigravity 預設設定；
不移用 API key 的每日限制或本機 token。原 Gemini CLI 安裝及個人紀錄不會被刪除。

Windows 自動偵測正在執行的 agy.exe，或安裝路徑屬於 Antigravity 的 language_server
程序及其 loopback 監聽埠。背景 PowerShell 使用隱藏視窗設定，不啟動模型對話。
先呼叫本機 LanguageServerService/RetrieveUserQuotaSummary；舊服務未實作時退回
GetUserStatus 的 cascadeModelConfigData.clientModelConfigs[].quotaInfo。新版 CLI 見下方用量報告備援。
不讀 Google OAuth 憑證、不直接傳送到雲端；若 Desktop 提供本機 CSRF 驗證值，
僅用於對應本機服務。拒絕 HTTP 轉址，不使用 HTTP proxy，不停用 TLS 驗證。

remainingFraction 為剩餘比例，顯示已使用百分比為 (1-remainingFraction)*100。
每個模型群的 5h / weekly 額度分開顯示，不將共享的模型額度相加。
主數字選擇使用比例最高的限制，並以模型群與視窗標示；其餘限制列於下方。
名稱含 Claude 的視窗另外在展開細節做成 Claude 模型進度條。
主機本機服務與 `agy` 用量報告都失敗時，改查執行中 WSL 發行版裡的 Linux `agy`
（`$HOME/.local/bin` 或 PATH，略過 `/mnt`）。沒有 Linux `agy` 時不報錯。
Gemini 的 WSL 來源尚未做。
CLI 和 Desktop 都開啟時採第一個成功回應的來源，來源會標示在畫面上，不合併帳號。

agy 1.2.2 起本機服務要求 CSRF 驗證，token 不在命令列也不寫入磁碟，只傳給 agy 的子程序；
外部程式送出的請求會得到 401（HTTPS 埠的 HTTP 請求則是 400）。本機服務查詢失敗時，
改執行官方非互動用量報告 `agy -p /usage --output-format json`：從 PATH 或
`%LOCALAPPDATA%\agy\bin\agy.exe` 找 CLI，在私有暫存目錄、背景隱藏視窗執行，
整體 30 秒逾時、stdout 上限 1 MiB、不讀 stderr；只接受 `status: SUCCESS` 且
`command.name: usage` 的 `command.data.groups[].buckets[]`（`remaining_fraction`、`reset_time`），
套用與本機服務相同的模型群規則。此報告不送出模型提示（`num_turns` 為 0）。
執行時把 `BROWSER` 指向不存在的程式，盡量避免登入失效時開啟瀏覽器；Windows 上 agy 是否遵守未確認。
用量報告不依賴 Windows 程序偵測，其他平台只要 PATH 有 `agy` 也能使用，但未在非 Windows 實機驗證。

成功每分鐘讀取一次；失敗 5 分鐘後再試，避免登入失效時每分鐘重試登入。
每個本機請求最多 3 秒、本機服務整輪最多 20 秒；重新掃描可立即重試。
Antigravity 必須已登入。關閉或錯誤時顯示原因及上次成功資料時間。
2026-10-16 在這台 Windows 的 Antigravity CLI 1.3.6 實機驗證：本機服務回 401 / 400，
改由用量報告取得 5 個額度視窗。先前 1.2.1 直接使用本機服務。Desktop 自動偵測與
舊介面解析已實作，但本機未找到 Desktop 安裝，故未完成 Desktop 實機驗證。
本機服務介面可能隨版本變動，若未回傳額度，不假造上限或使用量。

官方 CLI 用量說明：https://antigravity.google/docs/cli/commands/usage
