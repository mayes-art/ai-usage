# Gemini 桌面版額度

來源實作位於 `internal/source/gemini`。卡片排在 Cursor 上面。

Windows 讀取 `%APPDATA%\gemini` 的 Chromium 登入資料：`Local State` 的金鑰與
`Network\Cookies` 裡屬於 `google.com` 的登入 cookie。資料庫先複製到暫存目錄再唯讀查詢。桌面版若以獨占方式開啟 Cookies，
複製會失敗，畫面提示先關閉桌面版再重新掃描。
依 Cookies 資料庫版本解密；版本 24 起會驗證並移除主機名稱的 SHA-256 前綴，
避免把二進位雜湊當成 cookie 送出。唯讀開檔允許其他程序同時讀寫；真正獨占時仍需先關閉桌面版。
僅接受 `google.com` 與 `gemini.google.com` 的指定登入 cookie，值只留在記憶體。
先 GET `https://gemini.google.com/usage?hl=en`，取得頁面的驗證資訊，再 POST 固定的
`https://gemini.google.com/_/BardChatUi/data/batchexecute`，只呼叫 `jSf9Qc`（`GetUsageInfo`）。
全部拒絕轉址。
不刷新、不改寫、不另存登入資料。

依 Google 公開前端模組（2026-10-16 核對）解析額度回應的 bucket：種類 1 為 5 小時、
種類 2 為每週，比例為已使用的 0–1 值；種類 3 的額外 credits 不混入百分比。
主卡採用使用比例最高的一項，其標籤與重置時間採同一視窗，其餘放展開細節。
缺少比例不補 0%，缺少重置不推算。沒有可用額度時顯示讀取問題，不依對話次數估算。
此為內部介面，可能隨服務改版變動；未提供此功能的帳號可能無法讀取。

舊設定補入或已把 Gemini 放在尾端時，載入會將 Gemini 移到 Cursor 正上方；保留停用及其他設定值。

成功每分鐘讀一次，失敗 5 分鐘後再試；重新掃描可立即重試。
WSL 的 Gemini 來源尚未做。非 Windows 不讀桌面版登入。

參考：[Gemini 官方用量限制說明](https://support.google.com/gemini/answer/16275805?hl=en)、
[Chromium cookie schema](https://chromium.googlesource.com/chromium/src/+/main/net/extras/sqlite/sqlite_persistent_cookie_store.cc)。
