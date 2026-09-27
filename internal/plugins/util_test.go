package plugins

import (
	"strings"
	"testing"
)

func TestPluginBaseEnvKeepsOnlyAllowlistedVars(t *testing.T) {
	got := pluginBaseEnv([]string{
		"PATH=/usr/bin",
		"HOME=/home/concord",
		"LC_ALL=en_US.UTF-8",
		`SystemRoot=C:\Windows`, // Windows spells it in mixed case
		"GATEWAY_API_KEY=sk-secret",
		"CONCORD_ADMIN_PASSWORD=hunter2",
		"AWS_SECRET_ACCESS_KEY=abc",
		`=C:=C:\odd-windows-entry`,
	})
	joined := strings.Join(got, "\n")
	for _, keep := range []string{"PATH=/usr/bin", "HOME=/home/concord", "LC_ALL=en_US.UTF-8", `SystemRoot=C:\Windows`} {
		if !strings.Contains(joined, keep) {
			t.Errorf("expected %q to be inherited, got:\n%s", keep, joined)
		}
	}
	for _, drop := range []string{"GATEWAY_API_KEY", "CONCORD_ADMIN_PASSWORD", "AWS_SECRET_ACCESS_KEY", "odd-windows-entry"} {
		if strings.Contains(joined, drop) {
			t.Errorf("%s leaked into the plugin environment:\n%s", drop, joined)
		}
	}
}
