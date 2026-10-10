package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The Windows boot scripts are valid PowerShell (a path with a quote in
// it too), and the task runs the program from its folder into its log.
func TestBootTaskScriptsParse(t *testing.T) {
	pl := NewPlan(Platform{OS: Windows, Home: `C:\Users\O'Brien`})
	pl.ServerDir = `C:\Users\O'Brien\Concord\Server`
	s := pl.BootTaskScript(Server)
	for _, want := range []string{"-AtStartup", "'SYSTEM'", "[TimeSpan]::Zero", `concord-server.exe`, `server.log`, "New-NetFirewallRule", "Start-ScheduledTask", `-WorkingDirectory 'C:\Users\O''Brien\Concord\Server'`} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q:\n%s", want, s)
		}
	}
	if runtime.GOOS != "windows" {
		t.Skip("parsing needs PowerShell")
	}
	dir := t.TempDir()
	for name, script := range map[string]string{"boot.ps1": s, "remove.ps1": pl.BootTaskRemoval(Server)} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("powershell.exe", "-NoProfile", "-Command",
			"$e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('"+path+"', [ref]$null, [ref]$e); $e.Count").CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "0" {
			t.Fatalf("%s doesn't parse (%v): %s\n%s", name, err, out, script)
		}
	}
}
