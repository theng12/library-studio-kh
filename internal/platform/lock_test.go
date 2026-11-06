package platform

import "testing"

func TestLockConfigHomeIsExclusiveAndReleases(t *testing.T) {
	home := t.TempDir()
	release, err := LockConfigHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockConfigHome(home); err == nil {
		t.Fatal("a second host was allowed to lock the same configuration home")
	}
	release()
	releaseAgain, err := LockConfigHome(home)
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	releaseAgain()
}
