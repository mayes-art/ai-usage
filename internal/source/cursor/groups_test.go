package cursor

import (
	"testing"
	"time"
)

func TestIndependentModelPools(t *testing.T) {
	for _, tc := range []struct {
		name, body    string
		cursor, other float64 // -1 means unknown
		wantTotal     bool
	}{
		{"pools without a reported total", `{"planUsage":{"includedSpend":2000,"limit":2000,"autoPercentUsed":23,"apiPercentUsed":100}}`, 23, 100, false},
		{"live shape: low total, exhausted other", `{"planUsage":{"totalSpend":3624,"includedSpend":2000,"bonusSpend":1624,"limit":2000,"totalPercentUsed":14.496,"autoPercentUsed":0.5,"apiPercentUsed":89.875}}`, 0.5, 89.875, true},
		{"explicit zero", `{"planUsage":{"autoPercentUsed":0,"apiPercentUsed":0}}`, 0, 0, false},
		{"missing is not zero", `{"planUsage":{"autoPercentUsed":25}}`, 25, -1, false},
		{"legacy total only", `{"planUsage":{"totalPercentUsed":100}}`, -1, -1, true},
		{"pool amounts fallback", `{"planUsage":{"autoSpend":2500,"autoLimit":10000,"apiSpend":2000,"apiLimit":2000}}`, 25, 100, false},
		{"missing spend stays unknown", `{"planUsage":{"autoLimit":10000,"apiPercentUsed":10}}`, -1, 10, false},
		{"explicit percent wins", `{"planUsage":{"autoPercentUsed":15,"autoSpend":2500,"autoLimit":10000,"apiPercentUsed":125}}`, 15, 125, false},
		{"invalid pool stays unknown", `{"planUsage":{"autoPercentUsed":-1,"apiPercentUsed":10}}`, -1, 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := parseCursorUsage([]byte(tc.body), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if !r.Usable() || len(r.Groups) != 2 || (r.PercentUsed != nil) != tc.wantTotal {
				t.Fatalf("unexpected report: %+v", r)
			}
			if len(r.Windows) != 1 || r.Windows[0].HasPercent != tc.wantTotal {
				t.Fatalf("billing window percent presence: %+v", r.Windows)
			}
			for i, want := range []float64{tc.cursor, tc.other} {
				g := r.Groups[i]
				if want < 0 {
					if g.PercentUsed != nil {
						t.Fatalf("missing group became %v", *g.PercentUsed)
					}
				} else if g.PercentUsed == nil || *g.PercentUsed != want {
					t.Fatalf("group %d: %+v, want %v", i, g, want)
				}
			}
			if r.Groups[0].ID != "cursor-model" || r.Groups[1].ID != "other-model" {
				t.Fatal("pool identity changed")
			}
		})
	}
	r, err := parseCursorUsage([]byte(`{"planUsage":{"autoSpend":2500,"autoLimit":10000}}`), time.Now())
	if err != nil || r.Groups[0].Used == nil || *r.Groups[0].Used != 25 || *r.Groups[0].Limit != 100 {
		t.Fatalf("pool cents conversion: %+v, %v", r, err)
	}
	if _, err := parseCursorUsage([]byte(`{"planUsage":{"autoPercentUsed":-1,"apiLimit":0}}`), time.Now()); err == nil {
		t.Fatal("invalid pools must not become a usable report")
	}
}
