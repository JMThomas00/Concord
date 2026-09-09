package server

import "testing"

// TestResolveDashboardMode covers the --hybrid/--dashboard flag precedence
// rule Run() and cmd/server/main.go both rely on: hybrid wins if both are
// somehow set (it's the more capable of the two -- panels plus live
// scrolling logs, not a strict subset of full-screen mode), each flag alone
// maps to its own mode, and neither set means normal (no dashboard) mode.
func TestResolveDashboardMode(t *testing.T) {
	cases := []struct {
		name          string
		hybrid        bool
		dashboardOnly bool
		want          dashboardDisplayMode
	}{
		{"neither flag set", false, false, dashboardDisplayNone},
		{"hybrid only", true, false, dashboardDisplayHybrid},
		{"dashboard only", false, true, dashboardDisplayFull},
		{"both set, hybrid wins", true, true, dashboardDisplayHybrid},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDashboardMode(tc.hybrid, tc.dashboardOnly)
			if got != tc.want {
				t.Errorf("resolveDashboardMode(%v, %v) = %v, want %v", tc.hybrid, tc.dashboardOnly, got, tc.want)
			}
		})
	}
}

// TestSetFullDashboardMode confirms it mirrors SetDashboardMode's own
// enable/disable shape and doesn't clobber the other mode's state when
// toggled off (disabling one mode shouldn't disable the other if it wasn't
// the one active).
func TestSetFullDashboardMode(t *testing.T) {
	s := &Server{}

	s.SetFullDashboardMode(true)
	if s.dashboardDisplay != dashboardDisplayFull {
		t.Errorf("expected dashboardDisplayFull after enabling, got %v", s.dashboardDisplay)
	}
	if s.dashboard == nil {
		t.Error("expected a dashboard.Model to be constructed")
	}

	s.SetFullDashboardMode(false)
	if s.dashboardDisplay != dashboardDisplayNone {
		t.Errorf("expected dashboardDisplayNone after disabling, got %v", s.dashboardDisplay)
	}
}

// TestSetDashboardModeDoesNotClobberFullMode guards the branch in
// SetDashboardMode(false) that only resets to "none" if hybrid was the
// active mode -- calling it while full-screen mode is active must not
// silently turn full-screen mode off too.
func TestSetDashboardModeDoesNotClobberFullMode(t *testing.T) {
	s := &Server{}
	s.SetFullDashboardMode(true)

	s.SetDashboardMode(false)

	if s.dashboardDisplay != dashboardDisplayFull {
		t.Errorf("expected full-screen mode to remain active, got %v", s.dashboardDisplay)
	}
}
