package desktop

import (
	"slices"
	"testing"
)

func TestPanelBrowserIsolated(t *testing.T) {
	profile := `C:\Users\Test User\AppData\Roaming\aiusage\panel-browser`
	args := panelBrowserArgs("http://127.0.0.1:1234/?t=test", profile, 236, 595)
	for _, want := range []string{"--user-data-dir=" + profile, "--app=http://127.0.0.1:1234/?t=test", "--window-size=236,595", "--disable-background-mode", "--no-first-run"} {
		if !slices.Contains(args, want) {
			t.Errorf("missing isolated window argument %q", want)
		}
	}
}

func TestPanelSizeGrowsDownNotSideways(t *testing.T) {
	two := map[string]bool{"codex": true, "claude": true, "cursor": false}
	three := map[string]bool{"codex": true, "claude": true, "cursor": false, "antigravity": true}
	twoWidth, twoHeight := panelSize(two)
	threeWidth, threeHeight := panelSize(three)
	if twoWidth != threeWidth || twoWidth != panelCardWidth+panelSideChrome {
		t.Fatalf("來源數量不該改變寬度：%d -> %d", twoWidth, threeWidth)
	}
	if threeHeight-twoHeight != panelCardHeight+panelRowGap {
		t.Fatalf("多一個來源要多一張卡的高度與間距：%d -> %d", twoHeight, threeHeight)
	}

	withCursor := map[string]bool{"codex": true, "claude": true, "cursor": true}
	cursorWidth, cursorHeight := panelSize(withCursor)
	if cursorWidth != twoWidth {
		t.Fatalf("Cursor 不該把面板撐寬：%d -> %d", twoWidth, cursorWidth)
	}
	if cursorHeight-twoHeight != panelCardHeight+panelRowGap+panelCursorPools {
		t.Fatalf("Cursor 的兩個模型池需要額外高度：%d -> %d", twoHeight, cursorHeight)
	}

	if width, height := panelSize(nil); width != panelCardWidth+panelSideChrome || height != panelMinHeight {
		t.Fatalf("空面板尺寸：%d × %d", width, height)
	}
}

// 236 × 595 為實測值（內容 551.1 加標題列，overflows 為 false）。
// 改版面就要重新量；這個測試擋住無聲的回歸。
func TestDefaultPanelFitsWithoutScrollbar(t *testing.T) {
	const measuredContentHeight = 552 // 實測 551.1，進位
	all := map[string]bool{"claude": true, "codex": true, "cursor": true, "antigravity": true}
	width, height := panelSize(all)
	if width != 236 || height != 595 {
		t.Fatalf("四個來源的面板尺寸已改變：%d × %d，請重新實測內容高度", width, height)
	}
	if slack := height - panelWindowChrome - measuredContentHeight; slack < 0 {
		t.Fatalf("視窗比內容矮 %d px，預設狀態會出現捲軸", -slack)
	}
}

func TestPanelOpacityAndHoverBounds(t *testing.T) {
	if panelAlpha != 234 || panelAlphaHover != 255 {
		t.Fatalf("面板不透明度改變：預設 %d，滑入 %d", panelAlpha, panelAlphaHover)
	}
	// 右緣與下緣屬於下一個像素，否則相鄰視窗會同時算成滑入。
	cases := []struct {
		name string
		x, y int32
		want bool
	}{
		{"inside", 150, 200, true},
		{"top-left corner", 100, 100, true},
		{"left edge outside", 99, 200, false},
		{"right edge excluded", 400, 200, false},
		{"bottom edge excluded", 150, 500, false},
		{"above", 150, 99, false},
	}
	for _, c := range cases {
		if got := cursorOverPanel(c.x, c.y, 100, 100, 400, 500); got != c.want {
			t.Errorf("%s: cursorOverPanel(%d, %d) = %v", c.name, c.x, c.y, got)
		}
	}
	if !cursorOverPanel(-1820, 200, -1900, 100, -1600, 500) {
		t.Error("負座標的螢幕判定失敗")
	}
}
