//go:build !windows

package wsl

import (
	"context"
	"errors"
	"os/exec"
)

var errUnsupported = errors.New("WSL 僅支援 Windows")

func Run(context.Context, int, ...string) ([]byte, error) { return nil, errUnsupported }

func List(context.Context) ([]string, error) { return nil, nil }

func Exec(context.Context, string, int, string) ([]byte, error) { return nil, errUnsupported }

func Command(context.Context, string, string) (*exec.Cmd, error) { return nil, errUnsupported }
