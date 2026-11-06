//go:build !darwin

package platform

import (
	"errors"
	"testing"
)

func TestTrashIsExplicitlyUnsupportedAwayFromDarwin(t *testing.T) {
	if _, err := TrashDir(); !errors.Is(err, ErrTrashUnsupported) {
		t.Fatalf("TrashDir must report unsupported, got %v", err)
	}
}
