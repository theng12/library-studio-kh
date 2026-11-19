//go:build !darwin

package platform

import "errors"

var errHostServiceUnsupported = errors.New("the background host login job is macOS-only; this operating system is not verified")

func unsupportedStatus() HostServiceStatus {
	return HostServiceStatus{
		Supported: false,
		State:     HostServiceUnsupported,
		Label:     HostServiceLabel,
		Detail:    "The guided background host is macOS-only. Run the server directly with -service on this computer; Windows and Linux are not verified.",
	}
}

func (s *HostService) Status(HostServiceSpec) HostServiceStatus { return unsupportedStatus() }

func (s *HostService) Install(HostServiceSpec) (HostServiceStatus, error) {
	return unsupportedStatus(), errHostServiceUnsupported
}

func (s *HostService) Start(HostServiceSpec) (HostServiceStatus, error) {
	return unsupportedStatus(), errHostServiceUnsupported
}

func (s *HostService) Stop() (HostServiceStatus, error) {
	return unsupportedStatus(), errHostServiceUnsupported
}

func (s *HostService) Halt() (HostServiceStatus, error) {
	return unsupportedStatus(), errHostServiceUnsupported
}

func (s *HostService) Uninstall(string) (HostServiceStatus, error) {
	return unsupportedStatus(), errHostServiceUnsupported
}
