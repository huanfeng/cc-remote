package hostlock

import "testing"

func TestExclusiveAndRelease(t *testing.T) {
	dir := t.TempDir()
	release, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second, e := Acquire(dir); e == nil {
		second()
		release()
		t.Fatal("second host acquired data directory")
	}
	release()
	release2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("lock persisted after release: %v", err)
	}
	release2()
}

func TestHeld(t *testing.T) {
	dir := t.TempDir()
	if Held(dir) {
		t.Fatal("held without a lock file")
	}
	release, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !Held(dir) {
		t.Fatal("a running host's lock is not seen as held")
	}
	release()
	if Held(dir) {
		t.Fatal("held after release")
	}
	release, err = Acquire(dir)
	if err != nil {
		t.Fatalf("Held kept the lock: %v", err)
	}
	release()
}
