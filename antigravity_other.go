//go:build !windows

package main

import (
	"context"
	"fmt"
)

func discoverAntigravity(ctx context.Context) ([]antigravityEndpoint, error) {
	return nil, fmt.Errorf("Antigravity 本機服務自動偵測目前支援 Windows")
}
