# aiusage

把 Codex CLI、Claude Code、Gemini CLI、Cursor 的用量收在同一個面板上，
以 **用量 ÷ 總量的百分比** 呈現。

原理很簡單：這些工具都會在本機留下 JSON / JSONL 紀錄檔，這支程式定時去讀，
把各家不同名稱的 token 欄位收斂成同一組，再除以你設定的上限。
沒有資料庫，紀錄檔本身就是持久層，重開時重掃一次即可重建。

---

## 建置

純 Go，沒有 cgo，不需要任何外部相依。

```bash
# Windows x64，雙擊直接開面板（無主控台視窗）
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/aiusage-panel.exe .

# Windows x64，主控台版，report / doctor 這些指令要用它
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w" -o dist/aiusage.exe .
```

兩支都是單一執行檔，約 6.6 MB，`ui.html` 以 `go:embed` 包在裡面。

---

## 使用

### 開面板

雙擊 `aiusage-panel.exe`。它會挑一個本機埠，用 Edge 的 app 模式開一個沒有網址列的
視窗，資料預設每 3 秒更新一次。

重複雙擊不會開出第二個實例——它會偵測到既有的面板並把視窗叫回來。

> **關掉它**：關閉面板視窗**不會**結束程式。GUI 版沒有主控台，所以要到工作管理員
> 結束 `aiusage-panel.exe`。想要能 Ctrl+C 結束的話，改用 `aiusage.exe`。

開機自動啟動：`Win+R` → `shell:startup` → 把 `aiusage-panel.exe` 的捷徑丟進去。

### 設定上限

**沒有上限就沒有分母，那一列會顯示 `—%`。**

面板每一列右下角有「設定上限」按鈕，填數字按儲存，百分比立刻出現。
單位就是那一列顯示的單位：Codex / Claude 是 tokens，Gemini / Cursor 是次數。

Cursor 在本機不留可讀的用量紀錄，所以它是「已用」和「上限」兩格都要手填，
照 Cursor 後台看到的數字填即可。

如果某家工具真的把官方額度寫進本機紀錄，程式會自動採用它當分母，
面板上會標示「分母：紀錄中的官方回報值」，這種情況**不需要手動設定**。

**Codex 就屬於這一種。** OpenAI 沒有公布 Plus 的 token 上限（公布的是每 5 小時
的訊息數區間，還隨模型變動），但 Codex CLI 會把官方額度寫進每一筆 `token_count`
事件：

```json
"rate_limits": {
  "primary":   { "used_percent": 93.0, "window_minutes": 300,   "resets_at": ... },
  "secondary": { "used_percent": 46.0, "window_minutes": 10080, "resets_at": ... }
}
```

所以 Codex 的 `limit` 保持 0 就好。程式會把**短的視窗（5 小時）畫成進度條**，
長的視窗（7 天）放到細節列顯示成「7 天限額 46%」。

> 注意這種來源的百分比與底下的 token 數**不是同一個單位**：百分比是官方依訊息數
> 算的，token 數是本機自己統計的，所以標成「本機累計」。

### 主控台指令

```
aiusage.exe                  # 等同 panel
aiusage.exe report           # 在終端機印一次目前用量，不開視窗
aiusage.exe doctor           # 診斷：掃到哪些資料夾、命中哪些欄位
aiusage.exe config           # 印出設定檔路徑
aiusage.exe version
```

旗標（對 panel 有效）：

| 旗標 | 說明 |
| --- | --- |
| `-port N` | 指定本機埠，`0` 為自動挑選 |
| `-interval N` | 掃描間隔秒數 |
| `-no-browser` | 啟動後不要自動開視窗 |

---

## 設定檔

位置：`%APPDATA%\aiusage\config.json`（其他平台為 `~/.config/aiusage/config.json`）。
第一次執行時自動產生。改完存檔後，在面板上按「重新掃描」即可生效。

```jsonc
{
  "port": 0,                      // 0 = 自動挑一個可用埠
  "refresh_seconds": 3,
  "warn_ratio": 0.8,              // 進度條上的警戒線位置
  "prefer_reported_limits": true, // 紀錄裡有官方額度時優先採用
  "retention_days": 40,           // 超過這個天數的事件會被丟掉
  "providers": [
    {
      "id": "codex",
      "name": "Codex CLI",
      "enabled": true,
      "roots": ["C:\\Users\\you\\.codex\\sessions"],  // 要掃的資料夾，可多個
      "metric": "tokens",          // tokens | requests
      "window_kind": "rolling",    // rolling | day | week | month
      "window_seconds": 18000,     // rolling 專用
      "limit": 2000000             // 0 = 尚未設定
    }
  ]
}
```

同一個資料夾旁還會有兩個檔：`panel.log`（GUI 版的訊息都寫在這裡）和
`panel.url`（記錄執行中的面板網址，用來避免開出第二個實例）。

---

## 面板怎麼讀

```
● Claude Code   5 小時滾動視窗                        62%
                                          1.24M / 2.00M tokens
  ━━━━━━━━━━━━━━━━━━━━━━━━━━╸┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
  剩餘 38%（757.0k tokens）  最舊紀錄滑出 2 小時 14 分後  速率 180.0k / 小時
```

- **百分比**：用量 ÷ 總量。低於警戒線是藍色，超過警戒線轉黃，滿了轉紅。
- **進度條**：細長條，只負責讓比例一眼可見；上面那道細線是警戒線（`warn_ratio`）。
- **另一道限制**：某些來源同時有多個計量視窗（Codex 是 5 小時 + 7 天）。
  進度條畫最短的那個，其餘在細節列列出。
- **左側圓點**：實心 = 有用量、灰色 = 這個視窗內閒置、空心 = 讀不到資料。
- **滾動視窗 vs 今日／本週／本月**：由 `window_kind` 決定。滾動視窗顯示的是
  「最舊那筆紀錄什麼時候滑出視窗」，不是固定的重置時間。

---

## 讀不到資料時

先跑 `aiusage.exe doctor`。它會列出每個來源掃了哪些路徑、路徑存不存在、
讀了幾個檔、命中哪些 token 欄位名稱，以及有沒有找到疑似額度的欄位。

常見狀況：

- **找不到紀錄資料夾** — 該工具沒安裝，或紀錄放在別的位置。
  把正確路徑加進 config.json 對應來源的 `roots` 陣列。
- **資料夾在，但沒有可讀的紀錄檔** — 只讀 `.json` / `.jsonl` / `.ndjson`，
  且會跳過名稱含憑證字樣的檔案（見下方隱私）。
- **讀到檔案，但沒命中任何 token 欄位** — 該工具改了欄位名稱。
  `doctor` 的輸出會列出實際看到的欄位路徑，把它貼出來就能補進
  `extract.go` 的 `tokenFields` 字典。

`roots` 支援 `%APPDATA%` 與 `$HOME` 這類環境變數寫法。

---

## 隱私

- 所有資料只留在本機，不會上傳任何地方。
- 面板綁在 `127.0.0.1`，而且需要啟動時產生的隨機 token 才能存取，
  本機其他網頁打不到這個埠。
- 掃描時會跳過檔名含 `oauth`、`credential`、`token.json`、`secret`、
  `cookie`、`password`、`.env`、`id_rsa` 等字樣的檔案。
- `doctor` 只列出欄位**名稱**與統計數字，不會印出任何對話內容。
