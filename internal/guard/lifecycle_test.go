package guard

import (
	"github.com/YahirHub/svcforge"
	"testing"
)

func TestLifecycleRestartPolicies(t *testing.T) {
	c := fixtureConfig(t)
	for _, tc := range []struct {
		name    string
		manager svcforge.ServiceManager
		policy  svcforge.RestartPolicy
	}{
		{"OpenRC", svcforge.ServiceManagerOpenRC, svcforge.RestartNever},
		{"systemd", svcforge.ServiceManagerSystemd, svcforge.RestartOnFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := appForServiceManager(c, "/etc/tavito-guard/config.json", "0.1.1", tc.manager)
			if app.Service.Restart != tc.policy {
				t.Fatalf("política: %s, esperada: %s", app.Service.Restart, tc.policy)
			}
			if !app.Service.AutoStart || !app.Service.Enabled {
				t.Fatal("se desactivó el arranque del bot")
			}
			if err := app.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
