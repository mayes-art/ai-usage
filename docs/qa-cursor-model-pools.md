# Cursor 雙模型池驗證（2026-09-14）

## 行為

Cursor 主列下常駐 Cursor model 與 Other model 子項目，各自顯示百分比、進度條與來源提供的金額。方案總百分比放在詳細資訊。未知不當成零，兩池不合併。

## 已執行

- Docker Linux：`go test -race ./...` 與 `go vet ./...` 全部通過。
- 新增解析測試涵蓋總額 100%／分項 23%、明確零、部分缺值、舊版僅總額、各池金額計算、缺少已用值、百分比優先、超額與無效值。
- 服務測試涵蓋只有分項可用、比例轉換、快照隔離、45 分鐘警示、24 小時失效、手動偏好與停用。
- 終端輸出測試涵蓋零與未知；桌面高度測試涵蓋有／無 Cursor。
- 合成資料瀏覽器驗證：23% / 100%、0% / 0%、42% / 未知、較舊提示、停用、手動模式、125% / 80%、未安裝來源；子項目不需展開。展開後能看到原總百分比。
- 360 × 338 視窗檢查：單一 Cursor 卡片及兩個子項目完整顯示，無水平溢出。
- Windows amd64 主控台版、GUI 版及 macOS arm64 交叉建置通過。Windows 產物為 `dist/aiusage.exe`、`dist/aiusage-panel.exe`。

## Claude CLI QA

使用者完成登入後，已將 PR #2 的程式差異傳送至 Anthropic，由 Claude Sonnet 5 以停用工具、
禁止寫入及不發表 GitHub 評論的模式執行唯讀 QA。內建 `ultrareview` 當時不可用，
因此改用非互動程式檢視。

Claude 找到並已修正兩項問題：

- Release Please 原先會先建立 tag／草稿 Release，後續建置若失敗，重跑時不會再次進入建置。
  現改為 Release Please 只維護人工核准 PR；該 PR 合併後，另一個 job 先測試與建置，
  成功才建立 Release，且同一 workflow run 可重跑修復既有 Release 資產。
- Cursor 僅有分項、缺少方案總百分比時，帳單週期內部欄位會留下假的 `0%`。
  現以 `ReportedWindow.HasPercent` 明確區分未知與零，並增加回歸測試。

修正後 `actionlint`、`go test -race ./...` 與 `go vet ./...` 均再次通過，
並完成 Windows、Linux、macOS 的 amd64／arm64 六平台交叉建置。
Claude 針對修補差異再次檢視後回報 `PASS`，未發現可執行的新缺陷；首次實際
Release PR 合併仍是 GitHub Actions 事件排序、tag 與資產上傳的端對端驗證點。

## 範圍限制

未把憑證或個人對話傳送給 Claude。欄位對應核對本機 Cursor 3.17.21。
跨平台建置不等於 macOS 實機驗證；GitHub Actions 發布流程仍需在首張 Release PR 合併時完成端對端驗證。

## 2026-09-14 追加：實機核對

實機執行後發現方案總百分比固定顯示 100%，原因是把 `includedSpend` 當成已用金額；
已修正為採用 `totalPercentUsed`，詳見[檢視紀錄](review-2026-09-14.md)。
修正後實機 `report` 為 `14% US$2.90 / US$20.00`，兩個子項目 `0%` 與 `90%`，
與 Cursor 自己顯示的「14% of your included total usage」「90% of your included API usage」一致。
兩個模型池的數值在修正前後都正確，此次問題只在主列總量。
