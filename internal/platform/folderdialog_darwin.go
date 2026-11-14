//go:build darwin

package platform

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>

char *libraryPickFolder(const char *prompt);
*/
import "C"

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

// ErrFolderDialogUnsupported means this OS has no native folder panel; pages
// fall back to their in-app directory browser.
var ErrFolderDialogUnsupported = errors.New("the native folder panel is only available on macOS")

const folderDialogTimeout = 10 * time.Minute

type folderRequest struct {
	prompt string
	reply  chan folderResult
}

type folderResult struct {
	path string
	err  error
}

var (
	folderUIOnce   sync.Once
	folderRequests chan folderRequest
)

// StartFolderUI must be called once from main before serving. It keeps one
// goroutine on a locked OS thread for AppKit, because the Go host runs the
// server as a background sidecar and its other goroutines may move threads.
func StartFolderUI() {
	folderUIOnce.Do(func() {
		folderRequests = make(chan folderRequest)
		go func() {
			runtime.LockOSThread()
			for req := range folderRequests {
				req.reply <- pickFolderOnUIThread(req.prompt)
			}
		}()
	})
}

func pickFolderOnUIThread(prompt string) folderResult {
	p := C.CString(prompt)
	defer C.free(unsafe.Pointer(p))
	out := C.libraryPickFolder(p)
	if out == nil {
		return folderResult{} // cancelled
	}
	defer C.free(unsafe.Pointer(out))
	return folderResult{path: C.GoString(out)}
}

// PickFolder shows the native macOS folder panel and returns the chosen folder.
// An empty path with no error means the owner cancelled; the request is bounded
// so a closed panel can never hold a page open forever.
func PickFolder(prompt string) (string, error) {
	StartFolderUI()
	ctx, cancel := context.WithTimeout(context.Background(), folderDialogTimeout)
	defer cancel()
	req := folderRequest{prompt: prompt, reply: make(chan folderResult, 1)}
	select {
	case folderRequests <- req:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case res := <-req.reply:
		return res.path, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
