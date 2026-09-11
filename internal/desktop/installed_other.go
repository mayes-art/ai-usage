//go:build !windows && !darwin

package desktop

import (
	"aiusage/internal/model"
)

func InstalledProviders() map[string]bool {
	return map[string]bool{model.ProviderClaude: true, model.ProviderCodex: true, model.ProviderCursor: true, model.ProviderAntigravity: true}
}
