package main

import (
	"testing"

	"github.com/roudayna/ghostkey/pkg/dashboard"
)

// The dashboard shows a controller flag's default when the DaemonSet omits
// it: its copy of the defaults must follow the flag definitions.
func TestDashboardControllerFlagDefaults(t *testing.T) {
	for name, want := range dashboard.ControllerFlagDefaults {
		f := controllerCmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("controller has no --%s flag", name)
			continue
		}
		if f.DefValue != want {
			t.Errorf("--%s defaults to %q, dashboard assumes %q", name, f.DefValue, want)
		}
	}
}

func TestDashboardCommandFlags(t *testing.T) {
	for name, want := range map[string]string{
		"bind-address":              ":8090",
		"health-probe-bind-address": ":8091",
		"demo-data":                 "false",
	} {
		f := dashboardCmd.Flags().Lookup(name)
		if f == nil || f.DefValue != want {
			t.Errorf("--%s = %v, want default %q", name, f, want)
		}
	}
}
