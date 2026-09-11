package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Usage 是正規化後的 token 計數。各家 CLI 的欄位名稱不同，
// 這裡收斂成同一組欄位。
type Usage struct {
	Tool       int64 `json:"tool"`
	UseTotal   bool  `json:"-"`
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheWrite int64 `json:"cache_write"`
	CacheRead  int64 `json:"cache_read"`
	Reasoning  int64 `json:"reasoning"`
	Total      int64 `json:"total"`
}

// Billable 回傳用於進度條的 token 總量。
// 若來源只給 total，就用 total；否則自行加總分項。
func (u Usage) Billable() int64 {
	if u.UseTotal {
		return u.Total
	}
	sum := u.Input + u.Output + u.CacheWrite + u.CacheRead + u.Reasoning
	if sum == 0 {
		return u.Total
	}
	return sum
}

func (u Usage) empty() bool {
	return u.Input == 0 && u.Output == 0 && u.CacheWrite == 0 &&
		u.CacheRead == 0 && u.Reasoning == 0 && u.Total == 0
}

func (u *Usage) add(o Usage) {
	u.Tool += o.Tool
	u.UseTotal = u.UseTotal || o.UseTotal
	u.Input += o.Input
	u.Output += o.Output
	u.CacheWrite += o.CacheWrite
	u.CacheRead += o.CacheRead
	u.Reasoning += o.Reasoning
	u.Total += o.Total
}

// Event 是一筆去重後的用量事件。
type Event struct {
	Provider   string    `json:"provider"`
	At         time.Time `json:"at"`
	ApproxTime bool      `json:"approx_time"`
	Model      string    `json:"model"`
	Session    string    `json:"session"`
	Usage      Usage     `json:"usage"`
	Cumulative bool      `json:"cumulative"`
	Key        string    `json:"-"`
}

// Reported 是「來源自己回報的額度資訊」。
// 若某家 CLI 真的把剩餘額度寫進本機紀錄，就用它當進度條分母，
// 不必靠使用者手動填上限。
type Reported struct {
	Metric      string             `json:"metric,omitempty"`
	Source      string             `json:"source,omitempty"`
	At          time.Time          `json:"at"`
	PercentUsed *float64           `json:"percent_used,omitempty"`
	Used        *float64           `json:"used,omitempty"`
	Limit       *float64           `json:"limit,omitempty"`
	ResetAt     *time.Time         `json:"reset_at,omitempty"`
	Windows     []ReportedWindow   `json:"windows,omitempty"`
	Fields      map[string]float64 `json:"fields,omitempty"`
	Labels      map[string]string  `json:"labels,omitempty"`
}

// ReportedWindow 是一個獨立的計量視窗。
// 一家工具可以同時有好幾個：Codex 就同時回報 5 小時窗與 7 天窗，
// 兩組欄位名稱一模一樣，只差在上層是 primary 還是 secondary。
type ReportedWindow struct {
	Label         string     `json:"label"`
	PercentUsed   float64    `json:"percent_used"`
	WindowMinutes float64    `json:"window_minutes,omitempty"`
	ResetAt       *time.Time `json:"reset_at,omitempty"`
	hasPercent    bool
}

func (r Reported) usable() bool {
	if r.PercentUsed != nil {
		return true
	}
	return r.Used != nil && r.Limit != nil && *r.Limit > 0
}

// ---------------------------------------------------------------------------
// 欄位名稱字典
//
// 這些名稱是各家工具的內部實作細節，沒有穩定契約。因此比對方式刻意寬鬆：
// 正規化（去底線、轉小寫）後做「包含」比對，讓欄位改名時仍有機會命中。
// ---------------------------------------------------------------------------

func norm(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

type tokenField struct {
	aliases []string
	set     func(*Usage, int64)
}

// 順序重要：先比對較長、較具體的名稱，避免 "inputtokens" 被
// "cachecreationinputtokens" 誤搶或反之。
var tokenFields = []tokenField{
	{[]string{"cachecreationinputtokens", "cachecreationtokens", "cachewritetokens", "cachewriteinputtokens"},
		func(u *Usage, v int64) { u.CacheWrite += v }},
	{[]string{"cachereadinputtokens", "cachedcontenttokencount", "cachereadtokens", "cachedtokens", "cachedinputtokens"},
		func(u *Usage, v int64) { u.CacheRead += v }},
	{[]string{"reasoningtokens", "reasoningoutputtokens", "thoughtstokencount", "thinkingtokens"},
		func(u *Usage, v int64) { u.Reasoning += v }},
	{[]string{"inputtokens", "prompttokens", "prompttokencount", "inputtokencount"},
		func(u *Usage, v int64) { u.Input += v }},
	{[]string{"outputtokens", "completiontokens", "candidatestokencount", "outputtokencount"},
		func(u *Usage, v int64) { u.Output += v }},
	{[]string{"totaltokens", "totaltokencount"},
		func(u *Usage, v int64) { u.Total += v }},
}

func matchTokenField(key string) *tokenField {
	n := norm(key)
	for i := range tokenFields {
		for _, a := range tokenFields[i].aliases {
			if n == a {
				return &tokenFields[i]
			}
		}
	}
	// 退一步用包含比對，接住 "usage_input_tokens" 這類變體。
	for i := range tokenFields {
		for _, a := range tokenFields[i].aliases {
			if strings.Contains(n, a) {
				return &tokenFields[i]
			}
		}
	}
	return nil
}

var modelKeys = []string{"model", "modelname", "modelid", "modelslug", "modelversion"}
var timeKeys = []string{"timestamp", "createdat", "created", "occurredat", "startedat", "time", "date", "ts"}
var sessionKeys = []string{"sessionid", "session", "conversationid", "rolloutid", "threadid"}

// 去重用的識別碼，數字越小優先度越高。
var uidKeys = map[string]int{"requestid": 1, "messageid": 2, "uuid": 3, "eventid": 4, "id": 5}

// 額度／限制相關的訊號字。命中就記錄下來，doctor 會列出，
// 讓我們知道某家工具究竟有沒有把剩餘額度寫在本機。
// 注意欄位名稱的字序可能是任一種（used_percent / percent_used），
// 因此這裡收「percent」這個字本身，再於 buildReported 判斷語意。
var quotaSignals = []string{"ratelimit", "usagelimit", "quota", "percent", "utilization",
	"remaining", "resetsat", "resetat", "resettime", "resetsin", "limitresets",
	"windowseconds", "windowminutes", "windowdurationmins", "windowsize",
	"allowance", "credit", "balance"}

func looksQuota(key string) bool {
	n := norm(key)
	for _, s := range quotaSignals {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 遞迴走訪
// ---------------------------------------------------------------------------

type usageHit struct {
	path  string
	usage Usage
}

type extractCtx struct {
	hits        []usageHit
	quota       map[string]float64
	quotaLabels map[string]string
	model       string
	modelDepth  int
	session     string
	sessDepth   int
	uid         string
	uidRank     int
	at          time.Time
	atDepth     int
	keyPaths    map[string]int // 給 doctor 用：出現過的欄位路徑
	collectKeys bool
}

// ExtractRecord 從一筆已解析的 JSON 記錄中抽出用量與額度資訊。
func ExtractRecord(node any, collectKeys bool) ([]usageHit, *extractCtx) {
	ctx := &extractCtx{
		quota:       map[string]float64{},
		quotaLabels: map[string]string{},
		modelDepth:  1 << 30,
		sessDepth:   1 << 30,
		uidRank:     1 << 30,
		atDepth:     1 << 30,
		collectKeys: collectKeys,
	}
	if collectKeys {
		ctx.keyPaths = map[string]int{}
	}
	walk(node, "", 0, ctx)
	return ctx.hits, ctx
}

func walk(node any, path string, depth int, ctx *extractCtx) {
	if depth > 24 {
		return
	}
	switch v := node.(type) {
	case map[string]any:
		// 這個 map 本身是不是一個 usage 物件？
		if u, ok := usageFromMap(v); ok && !strings.Contains(norm(lastSeg(path)), "details") {
			ctx.hits = append(ctx.hits, usageHit{path: path, usage: u})
			// 不再往下遞迴：巢狀的 *_details 是上層的子集，
			// 繼續往下會重複計算。
			scanMeta(v, path, depth, ctx)
			return
		}
		for k, child := range v {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if ctx.collectKeys {
				ctx.keyPaths[p]++
			}
			collectMeta(k, child, p, depth, ctx)
			walk(child, p, depth+1, ctx)
		}
	case []any:
		for i, child := range v {
			p := path
			if i < 8 { // 陣列只展開前幾個元素，避免大檔案炸開
				walk(child, p, depth+1, ctx)
			}
		}
	}
}

func lastSeg(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func scanMeta(m map[string]any, path string, depth int, ctx *extractCtx) {
	for k, child := range m {
		p := k
		if path != "" {
			p = path + "." + k
		}
		// usage 物件本身的欄位也要記錄，diagnose 才知道命中了哪些名稱。
		if ctx.collectKeys {
			ctx.keyPaths[p]++
		}
		collectMeta(k, child, p, depth, ctx)
	}
}

func collectMeta(key string, val any, path string, depth int, ctx *extractCtx) {
	n := norm(key)
	// 解碼時用了 UseNumber，數字會是 json.Number，先轉成 float64
	// 否則下面的 type switch 全部落空。
	if jn, ok := val.(json.Number); ok {
		if f, err := jn.Float64(); err == nil {
			val = f
		} else {
			val = jn.String()
		}
	}

	if looksQuota(key) {
		switch tv := val.(type) {
		case float64:
			ctx.quota[path] = tv
		case string:
			if t, ok := parseTimeString(tv); ok {
				ctx.quota[path] = float64(t.Unix())
				ctx.quotaLabels[path] = tv
			} else if len(tv) < 48 {
				ctx.quotaLabels[path] = tv
			}
		case bool:
			if tv {
				ctx.quota[path] = 1
			} else {
				ctx.quota[path] = 0
			}
		}
	}

	if s, ok := val.(string); ok && s != "" {
		for _, mk := range modelKeys {
			if n == mk && depth < ctx.modelDepth && len(s) < 80 {
				ctx.model, ctx.modelDepth = s, depth
			}
		}
		for _, sk := range sessionKeys {
			if n == sk && depth < ctx.sessDepth && len(s) < 120 {
				ctx.session, ctx.sessDepth = s, depth
			}
		}
		if rank, ok := uidKeys[n]; ok && len(s) < 120 && rank < ctx.uidRank {
			ctx.uid, ctx.uidRank = s, rank
		}
		for _, tk := range timeKeys {
			if n == tk && depth <= ctx.atDepth {
				if t, ok := parseTimeString(s); ok {
					ctx.at, ctx.atDepth = t, depth
				}
			}
		}
	}
	if f, ok := val.(float64); ok {
		for _, tk := range timeKeys {
			if n == tk && depth <= ctx.atDepth {
				if t, ok := epochToTime(f); ok {
					ctx.at, ctx.atDepth = t, depth
				}
			}
		}
	}
}

func usageFromMap(m map[string]any) (Usage, bool) {
	var u Usage
	matched := 0
	for k, v := range m {
		f, ok := toNumber(v)
		if !ok {
			continue
		}
		if tf := matchTokenField(k); tf != nil {
			tf.set(&u, int64(f))
			matched++
		}
	}
	if matched == 0 {
		return u, false
	}
	return u, true
}

func toNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) || t < 0 {
			return 0, false
		}
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	}
	return 0, false
}

var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999Z0700",
	"2006-01-02 15:04:05.999999-07:00",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
}

func parseTimeString(s string) (time.Time, bool) {
	if len(s) < 8 || len(s) > 40 {
		return time.Time{}, false
	}
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func epochToTime(f float64) (time.Time, bool) {
	switch {
	case f > 1e17: // 奈秒
		return time.Unix(0, int64(f)).UTC(), true
	case f > 1e14: // 微秒
		return time.UnixMicro(int64(f)).UTC(), true
	case f > 1e11: // 毫秒
		return time.UnixMilli(int64(f)).UTC(), true
	case f > 1e9: // 秒
		return time.Unix(int64(f), 0).UTC(), true
	}
	return time.Time{}, false
}

// ---------------------------------------------------------------------------
// 累計 vs 增量
//
// 有些工具（例如 Codex 的 token_count 事件）同時寫出「本次用量」與
// 「整個 session 的累計用量」。兩者相加會嚴重高估，必須擇一。
// ---------------------------------------------------------------------------

func classifyHits(hits []usageHit) (usage Usage, cumulative bool) {
	var deltas, cums []usageHit
	for _, h := range hits {
		if isCumulativePath(h.path) {
			cums = append(cums, h)
		} else {
			deltas = append(deltas, h)
		}
	}
	// 有增量就只用增量。
	if len(deltas) > 0 {
		for _, h := range deltas {
			usage.add(h.usage)
		}
		return usage, false
	}
	for _, h := range cums {
		usage.add(h.usage)
	}
	return usage, len(cums) > 0
}

func isCumulativePath(path string) bool {
	n := norm(path)
	if strings.Contains(n, "last") {
		return false
	}
	return strings.Contains(n, "totaltokenusage") ||
		strings.Contains(n, "cumulative") ||
		strings.Contains(n, "sessiontotal") ||
		strings.Contains(n, "totalusage")
}

// ---------------------------------------------------------------------------
// 從一筆記錄產生 Event / Reported
// ---------------------------------------------------------------------------

// ParseRecord 把一筆 JSON 記錄轉成事件。fallbackTime 通常是檔案的 mtime。
func ParseRecord(provider string, raw []byte, fallbackTime time.Time, collectKeys bool) (*Event, *Reported, map[string]int) {
	var node any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&node); err != nil {
		return nil, nil, nil
	}
	return parseNode(provider, node, fallbackTime, collectKeys, hashBytes(raw))
}

func parseNode(provider string, node any, fallbackTime time.Time, collectKeys bool, rawHash string) (*Event, *Reported, map[string]int) {
	hits, ctx := ExtractRecord(node, collectKeys)

	var rep *Reported
	if len(ctx.quota) > 0 || len(ctx.quotaLabels) > 0 {
		rep = buildReported(ctx, fallbackTime)
	}
	if provider == providerClaude {
		at := ctx.at
		if at.IsZero() {
			at = fallbackTime
		}
		rep = parseClaudeQuota(node, at)
	}

	if len(hits) == 0 {
		return nil, rep, ctx.keyPaths
	}
	usage, cum := classifyHits(hits)
	if usage.empty() {
		return nil, rep, ctx.keyPaths
	}
	at := ctx.at
	approx := false
	if at.IsZero() {
		at, approx = fallbackTime, true
	}
	key := provider + "|h|" + rawHash
	if ctx.uid != "" {
		key = provider + "|u|" + ctx.uid
	}
	ev := &Event{
		Provider:   provider,
		At:         at,
		ApproxTime: approx,
		Model:      ctx.model,
		Session:    ctx.session,
		Usage:      usage,
		Cumulative: cum,
		Key:        key,
	}
	return ev, rep, ctx.keyPaths
}

// windowLabel 把視窗長度講成人話：300 分 → 「5 小時」，10080 分 → 「7 天」。
func windowLabel(minutes float64) string {
	m := int(math.Round(minutes))
	switch {
	case m <= 0:
		return ""
	case m%1440 == 0:
		return fmt.Sprintf("%d 天", m/1440)
	case m%60 == 0:
		return fmt.Sprintf("%d 小時", m/60)
	default:
		return fmt.Sprintf("%d 分", m)
	}
}

func buildReported(ctx *extractCtx, at time.Time) *Reported {
	r := &Reported{At: at, Fields: map[string]float64{}, Labels: map[string]string{}}
	for k, v := range ctx.quota {
		r.Fields[k] = v
	}
	for k, v := range ctx.quotaLabels {
		r.Labels[k] = v
	}
	if !ctx.at.IsZero() {
		r.At = ctx.at
	}

	// 依欄位的「上層路徑」分組，一組就是一個計量視窗。
	// 不分組的話，rate_limits.primary 與 rate_limits.secondary 的欄位名稱
	// 完全一樣，Go 的 map 迭代順序又是隨機的，面板會在兩個百分比之間亂跳。
	groups := map[string]*ReportedWindow{}
	for k, v := range r.Fields {
		parent := ""
		if i := strings.LastIndexByte(k, '.'); i >= 0 {
			parent = k[:i]
		}
		g := groups[parent]
		if g == nil {
			g = &ReportedWindow{}
			groups[parent] = g
		}
		leaf := norm(lastSeg(k))
		switch {
		case strings.Contains(leaf, "percent") && (strings.Contains(leaf, "used") || strings.Contains(leaf, "utilization")):
			if v >= 0 && v <= 100 {
				g.PercentUsed, g.hasPercent = v, true
			}
		case strings.Contains(leaf, "percent") && strings.Contains(leaf, "remaining"):
			if v >= 0 && v <= 100 {
				g.PercentUsed, g.hasPercent = 100-v, true
			}
		case strings.Contains(leaf, "windowminutes"), strings.Contains(leaf, "windowdurationmins"):
			g.WindowMinutes = v
		case strings.Contains(leaf, "windowseconds"), strings.Contains(leaf, "windowsize"):
			g.WindowMinutes = v / 60
		case strings.Contains(leaf, "reset"):
			if v > 1e9 {
				if t, ok := epochToTime(v); ok {
					g.ResetAt = &t
				}
			}
		}
	}

	for _, g := range groups {
		if !g.hasPercent {
			continue
		}
		g.Label = windowLabel(g.WindowMinutes)
		r.Windows = append(r.Windows, *g)
	}
	// App-server may expose the same quota through both the legacy rateLimits
	// field and rateLimitsByLimitId. Keep one copy of each identical window.
	if len(r.Windows) > 1 {
		seen := map[string]bool{}
		unique := r.Windows[:0]
		for _, w := range r.Windows {
			reset := int64(0)
			if w.ResetAt != nil {
				reset = w.ResetAt.Unix()
			}
			key := fmt.Sprintf("%.0f|%.6f|%d", w.WindowMinutes, w.PercentUsed, reset)
			if seen[key] {
				continue
			}
			seen[key] = true
			unique = append(unique, w)
		}
		r.Windows = unique
	}
	// 短的視窗排前面：它先滿，也就是實際卡住你的那一個。
	// 長度未知的排最後，順序再以百分比高低固定下來，避免同分時抖動。
	sort.Slice(r.Windows, func(i, j int) bool {
		a, b := r.Windows[i], r.Windows[j]
		ak, bk := a.WindowMinutes > 0, b.WindowMinutes > 0
		if ak != bk {
			return ak
		}
		if ak && a.WindowMinutes != b.WindowMinutes {
			return a.WindowMinutes < b.WindowMinutes
		}
		if a.PercentUsed != b.PercentUsed {
			return a.PercentUsed > b.PercentUsed
		}
		return a.Label < b.Label
	})
	if len(r.Windows) > 0 {
		pv := r.Windows[0].PercentUsed
		r.PercentUsed = &pv
		r.ResetAt = r.Windows[0].ResetAt
	}
	// 沒有任何百分比時，至少把重置時間撿回來。
	if r.PercentUsed == nil {
		for k, v := range r.Fields {
			if strings.Contains(norm(k), "reset") && v > 1e9 {
				if t, ok := epochToTime(v); ok {
					r.ResetAt = &t
					break
				}
			}
		}
	}
	// used / limit 成對出現時也可用。
	for k, v := range r.Fields {
		n := norm(k)
		if strings.HasSuffix(n, "limit") && v > 0 {
			base := strings.TrimSuffix(k, k[len(k)-len("limit"):])
			for k2, v2 := range r.Fields {
				n2 := norm(k2)
				if strings.HasPrefix(k2, base) && (strings.HasSuffix(n2, "used") || strings.HasSuffix(n2, "usage")) {
					lv, uv := v, v2
					r.Limit, r.Used = &lv, &uv
				}
			}
		}
	}
	if len(r.Fields) == 0 && len(r.Labels) == 0 {
		return nil
	}
	return r
}
