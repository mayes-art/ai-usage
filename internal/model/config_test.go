package model

import "testing"

func TestConfigCloneIsIndependent(t *testing.T) {
	original := &Config{Providers: []ProviderConfig{{ID: ProviderCursor, Roots: []string{"original"}, Manual: &ManualUsage{Used: 3}}}}
	copy := original.Clone()
	provider := copy.Provider(ProviderCursor)
	provider.Roots[0] = "edited"
	provider.Manual.Used = 9
	provider.Name = "edited"
	if original.Providers[0].Roots[0] != "original" || original.Providers[0].Manual.Used != 3 || original.Providers[0].Name != "" {
		t.Fatalf("editing clone mutated original: %+v", original)
	}
}
