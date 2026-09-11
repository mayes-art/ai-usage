package antigravity

import (
	"aiusage/internal/desktop"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Only enumerate listeners owned by agy or an Antigravity language server.
// stdout stays in memory; an optional local CSRF value is never logged.
func discoverAntigravity(ctx context.Context) ([]antigravityEndpoint, error) {
	const script = `$ErrorActionPreference='Stop'
$all=Get-CimInstance Win32_Process
$targets=@($all | Where-Object { $_.Name -eq 'agy.exe' -or ($_.Name -like 'language_server*' -and $_.ExecutablePath -match '(?i)antigravity') })
$result=@()
foreach($p in $targets){
 $csrf=''
 if($p.CommandLine -match '--csrf_token(?:=|\s+)([^\s"]+)'){$csrf=$Matches[1]}
 $source=if($p.Name -eq 'agy.exe'){'Antigravity CLI'}else{'Antigravity Desktop'}
 foreach($c in @(Get-NetTCPConnection -OwningProcess $p.ProcessId -State Listen -ErrorAction SilentlyContinue)) {
  if($c.LocalAddress -eq '127.0.0.1' -or $c.LocalAddress -eq '::1') {$result+=@{port=[int]$c.LocalPort;source=$source;csrf=$csrf}}
 }
}
ConvertTo-Json -InputObject @($result) -Compress`
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	desktop.ConfigureBackgroundProcess(cmd)
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("無法偵測 Antigravity 本機服務；請確認 CLI 或 Desktop 正在執行")
	}
	var endpoints []antigravityEndpoint
	if json.Unmarshal(data, &endpoints) != nil {
		return nil, fmt.Errorf("Antigravity 本機服務偵測回傳格式異常")
	}
	if len(endpoints) > 12 {
		endpoints = endpoints[:12]
	}
	return endpoints, nil
}
