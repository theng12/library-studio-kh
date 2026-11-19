package main

import (
	"encoding/json"
	"fmt"
	"os"

	"librarystudio/internal/platform"
)

// hostControlOutput is the JSON contract for guided setup and the job UI.
// It deliberately has no token and no listen address.
type hostControlOutput struct {
	OK           bool                      `json:"ok"`
	Supported    bool                      `json:"supported"`
	State        platform.HostServiceState `json:"state"`
	Label        string                    `json:"label,omitempty"`
	Installed    bool                      `json:"installed"`
	Loaded       bool                      `json:"loaded"`
	Running      bool                      `json:"running"`
	NeedsRestart bool                      `json:"needsRestart"`
	Message      string                    `json:"message,omitempty"`
	Error        string                    `json:"error,omitempty"`
}

func hostControlService() *platform.HostService {
	return platform.NewHostService(platform.ExecRunner{}, func(home string) error {
		_, err := attach(home)
		return err
	})
}

func runHostControl(command string, svc *platform.HostService, spec platform.HostServiceSpec) (hostControlOutput, int) {
	var st platform.HostServiceStatus
	var err error
	switch command {
	case "install":
		st, err = svc.Install(spec)
	case "start":
		st, err = svc.Start(spec)
	case "stop":
		checked := svc.Status(platform.HostServiceSpec{Home: spec.Home})
		if checked.State == platform.HostServiceBlocked {
			st, err = checked, fmt.Errorf("%s", checked.Detail)
		} else {
			st, err = svc.Stop()
		}
	case "halt":
		// Stops the process but keeps the login job loaded, so an installer can
		// unload it itself instead of finding it gone and refusing (issues row
		// 20261120-139). This is what Quit uses.
		checked := svc.Status(platform.HostServiceSpec{Home: spec.Home})
		if checked.State == platform.HostServiceBlocked {
			st, err = checked, fmt.Errorf("%s", checked.Detail)
		} else {
			st, err = svc.Halt()
		}
	case "uninstall":
		st, err = svc.Uninstall(spec.Home)
	case "status":
		st = svc.Status(platform.HostServiceSpec{Home: spec.Home})
	default:
		return hostControlOutput{
			Supported: true,
			State:     platform.HostServiceBlocked,
			Error:     fmt.Sprintf("unknown host-control command %q; use install, start, stop, halt, uninstall or status", command),
		}, 1
	}
	out := hostControlOutput{
		OK:           err == nil,
		Supported:    st.Supported,
		State:        st.State,
		Label:        st.Label,
		Installed:    st.Installed,
		Loaded:       st.Loaded,
		Running:      st.Running,
		NeedsRestart: st.NeedsRestart,
		Message:      st.Detail,
	}
	if err != nil {
		out.Error = err.Error()
		return out, 1
	}
	if !st.Supported && command != "status" {
		return out, 2
	}
	return out, 0
}

func printHostControl(out hostControlOutput) {
	_ = json.NewEncoder(os.Stdout).Encode(out)
}
