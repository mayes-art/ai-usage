//go:build !windows

package claude

import "context"

func listClaudeWSL(context.Context) ([]string, error)       { return nil, nil }
func readClaudeWSL(context.Context, string) ([]byte, error) { return nil, nil }
