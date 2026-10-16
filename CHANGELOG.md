# Changelog

## [0.3.0](https://github.com/mayes-art/ai-usage/compare/v0.3.0...v0.3.0) (2026-10-16)


### Features

* add reviewed release PR workflow ([5882c2a](https://github.com/mayes-art/ai-usage/commit/5882c2a511c0dababd887570dab89b483683524a))
* AI 用量顯示工具 ([b45bb61](https://github.com/mayes-art/ai-usage/commit/b45bb61054e45b7fbfb66ab4fae19d218c261cad))
* AI用量顯示 ([ef59fab](https://github.com/mayes-art/ai-usage/commit/ef59fab1bbf6bcee73de303afd70375e8bbcb8e1))
* Claude 即時查詢帳號額度並支援 WSL 備援，修正新版 Antigravity CLI 無法顯示用量 ([6cd5853](https://github.com/mayes-art/ai-usage/commit/6cd5853d60ab0a2eb3fddda3c81e0e8de0401aa4))
* **panel:** redesign as a vertical glass dashboard with window controls ([37d8751](https://github.com/mayes-art/ai-usage/commit/37d87517df6a5c9708fa9d25ad6ef2e4dbba0c26))
* show separate Cursor model usage pools ([19b6a8e](https://github.com/mayes-art/ai-usage/commit/19b6a8ee80c87cbfc7bed1826741f78ef4345cef))
* track account quota reset times separately from local windows ([cbb4a21](https://github.com/mayes-art/ai-usage/commit/cbb4a21bccab00e50aec4697101126db72d2078c))
* 修改README.md ([e56a45e](https://github.com/mayes-art/ai-usage/commit/e56a45edf3cd259c50682616eba9962273e67754))
* 加入 Gemini 桌面版額度與 Codex、Antigravity WSL 備援 ([00d1a41](https://github.com/mayes-art/ai-usage/commit/00d1a415266083066a5364d0b2e4de60ec8ab075))


### Bug Fixes

* make reviewed releases recoverable ([4c488b4](https://github.com/mayes-art/ai-usage/commit/4c488b4fed8a227ce0ed68be3a094650d68abbce))

## [0.3.0](https://github.com/mayes-art/ai-usage/compare/v0.2.0...v0.3.0) (2026-10-16)

### Features

* 新增 Gemini 桌面版帳號額度，顯示 5 小時與每週用量、重置時間及查詢狀態；卡片位於 Cursor 上方。
* Codex 與 Antigravity 支援執行中 WSL 的 Linux CLI 備援，主機與 WSL 額度分開辨識、不相加。
* Antigravity 展開細節新增 Claude 模型用量。
* Claude 沿用 Code 檔案式登入定期查詢共享帳號額度，Windows 主機登入不可用時可改用 WSL 登入。

### Bug Fixes

* 修正 Gemini 新版 Chromium cookie 的主機雜湊解密，以及用量頁改採 GetUsageInfo 查詢的解析方式。
* 修正升級後 Gemini 被附加到來源尾端的排序，保留既有設定與停用狀態。
* 修正新版 Antigravity CLI 本機服務需要 CSRF 而無法取得用量，改用 CLI 內建用量報告備援。
* Claude 主卡固定顯示 5 小時額度，避免過期視窗或週額度取代主值；到期過濾僅套用明確啟用的來源。

### Documentation

* 更新來源整合、WSL 使用條件、設定遷移與登入資料處理說明，補齊回歸測試及使用者驗收紀錄。

## [0.2.0](https://github.com/mayes-art/ai-usage/compare/v0.1.2...v0.2.0) (2026-09-15)


### Features

* 全新垂直玻璃面板，支援卡片展開、來源圖示、視窗置頂、透明度切換與面板空白處拖曳 ([37d8751](https://github.com/mayes-art/ai-usage/commit/37d87517df6a5c9708fa9d25ad6ef2e4dbba0c26))
* 分離帳號額度與本機統計視窗的重置時間，加入即時倒數與到期自動重查 ([cbb4a21](https://github.com/mayes-art/ai-usage/commit/cbb4a21bccab00e50aec4697101126db72d2078c))


### Documentation

* 將一般使用指南與開發技術細節拆分，讓下載、操作及維護資訊更容易查找 ([adb9251](https://github.com/mayes-art/ai-usage/commit/adb925126bae25083541f128e564118f21248e19))

## [0.1.2](https://github.com/mayes-art/ai-usage/compare/v0.1.1...v0.1.2) (2026-09-14)


### Features

* add reviewed release PR workflow ([5882c2a](https://github.com/mayes-art/ai-usage/commit/5882c2a511c0dababd887570dab89b483683524a))


### Bug Fixes

* make reviewed releases recoverable ([4c488b4](https://github.com/mayes-art/ai-usage/commit/4c488b4fed8a227ce0ed68be3a094650d68abbce))
