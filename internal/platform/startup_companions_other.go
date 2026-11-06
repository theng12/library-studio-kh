//go:build !darwin

package platform

import "errors"

var errCompanionUnsupported = errors.New("companion at-login startup is macOS-only; this operating system is not verified")

func unsupportedCompanionStatus(spec CompanionServiceSpec) CompanionServiceStatus {
	return CompanionServiceStatus{
		Supported: false,
		State:     HostServiceUnsupported,
		ID:        spec.ID,
		Label:     CompanionServiceLabel(spec.ID),
		Detail:    "Companion at-login startup is macOS-only. Windows and Linux are not verified.",
	}
}

func (s *CompanionService) Status(spec CompanionServiceSpec) CompanionServiceStatus {
	return unsupportedCompanionStatus(spec)
}

func (s *CompanionService) Install(spec CompanionServiceSpec) (CompanionServiceStatus, error) {
	return unsupportedCompanionStatus(spec), errCompanionUnsupported
}

func (s *CompanionService) Remove(spec CompanionServiceSpec) (CompanionServiceStatus, error) {
	return unsupportedCompanionStatus(spec), errCompanionUnsupported
}
