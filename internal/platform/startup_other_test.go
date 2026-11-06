//go:build !darwin

package platform

import "testing"

func TestHostServiceReportsUnsupportedExplicitly(t *testing.T) {
	svc := NewHostService(nil, nil)
	st := svc.Status(HostServiceSpec{Home: "/tmp/library-studio"})
	if st.Supported || st.State != HostServiceUnsupported {
		t.Fatalf("unsupported status = %+v", st)
	}
	if _, err := svc.Install(HostServiceSpec{Home: "/tmp/library-studio", Port: 7881}); err == nil {
		t.Fatal("install claimed to work on an unsupported OS")
	}
	if _, err := svc.Start(HostServiceSpec{Home: "/tmp/library-studio", Port: 7881}); err == nil {
		t.Fatal("start claimed to work on an unsupported OS")
	}
	if _, err := svc.Stop(); err == nil {
		t.Fatal("stop claimed to work on an unsupported OS")
	}
}
