package setup

import (
	"os"
	"testing"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/platform"
)

func TestStartupUsesOwnExecutableAndOnlyNewManagedCompanions(t *testing.T) {
	m, _ := managerFixture(t)
	m.SetHostPort(18881)
	m.job.View.Apps[0].Managed = true
	m.job.View.Apps = append(m.job.View.Apps, AppStep{ID: companioninstall.Sonarr, Name: "Sonarr", Managed: false})
	calls := 0
	m.installCompanion = func(spec platform.CompanionServiceSpec) (platform.CompanionServiceStatus, error) {
		calls++
		if spec.ID != "qbittorrent" || spec.Home != m.home || !spec.AtLogin {
			t.Fatal("wrong companion selected for startup")
		}
		return platform.CompanionServiceStatus{Supported: true, Installed: true, Loaded: true, State: platform.HostServiceLoaded}, nil
	}
	m.installHost = func(spec platform.HostServiceSpec) (platform.HostServiceStatus, error) {
		exe, _ := os.Executable()
		if spec.Exe != exe || spec.Port != 18881 || spec.Home != m.home {
			t.Fatal("startup did not use the running server's actual values")
		}
		return platform.HostServiceStatus{Supported: true, Installed: true, Loaded: true, State: platform.HostServiceLoaded}, nil
	}
	ready, err := m.installStartup()
	if err != nil || !ready || calls != 1 {
		t.Fatal("managed startup configuration did not finish")
	}
}

func TestStartupRequiresRestartInsteadOfClaimingDesktopHandoff(t *testing.T) {
	m, _ := managerFixture(t)
	m.SetHostPort(18882)
	m.installCompanion = func(platform.CompanionServiceSpec) (platform.CompanionServiceStatus, error) {
		t.Fatal("adopted companion startup changed")
		return platform.CompanionServiceStatus{}, nil
	}
	m.installHost = func(platform.HostServiceSpec) (platform.HostServiceStatus, error) {
		return platform.HostServiceStatus{Supported: true, Installed: true, NeedsRestart: true, State: platform.HostServiceNeedsRestart}, nil
	}
	ready, err := m.installStartup()
	job, _ := m.Status()
	if err != nil || ready || job.State != "needs_restart" {
		t.Fatal("unverified handoff looked ready")
	}
	if len(job.Checks) != 1 || job.Checks[0].State != "not_checked" {
		t.Fatal("unverified startup check looked passed")
	}
}
