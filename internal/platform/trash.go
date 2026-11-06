package platform

import "errors"

// ErrTrashUnsupported means this OS has no native Trash helper here, so the
// staged cleanup engine must report native-Trash work as unsupported rather
// than pretend it succeeded.
var ErrTrashUnsupported = errors.New("the system Trash isn't supported on this OS")
