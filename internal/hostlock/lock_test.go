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
