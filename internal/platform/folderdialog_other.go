//go:build !darwin

package platform

import "errors"

// ErrFolderDialogUnsupported means this OS has no native folder panel; pages
// fall back to their in-app directory browser.
var ErrFolderDialogUnsupported = errors.New("the native folder panel is only available on macOS")

// StartFolderUI is a no-op where there is no native panel to service.
func StartFolderUI() {}

// PickFolder reports the missing native panel so the page opens its directory browser.
func PickFolder(string) (string, error) { return "", ErrFolderDialogUnsupported }
