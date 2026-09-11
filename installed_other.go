//go:build !windows && !darwin

package main

func installedProviders() map[string]bool {
	return map[string]bool{providerClaude: true, providerCodex: true, providerCursor: true, providerAntigravity: true}
}
