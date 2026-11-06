//go:build !darwin

package platform

import "testing"

func TestCompanionServiceReportsUnsupportedExplicitly(t *testing.T) {
	svc := NewCompanionService(nil)
	spec := CompanionServiceSpec{ID: CompanionSonarr, Home: "/tmp/library-studio", AtLogin: true}
	if st := svc.Status(spec); st.Supported || st.State != HostServiceUnsupported {
		t.Fatalf("unsupported status = %+v", st)
	}
	if _, err := svc.Install(spec); err == nil {
		t.Fatal("install claimed to work on an unsupported OS")
	}
	if _, err := svc.Remove(spec); err == nil {
		t.Fatal("remove claimed to work on an unsupported OS")
	}
}
