package platform

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestRaiseFDLimitReportsSupported(t *testing.T) {
	limits, err := RaiseFDLimit()
	if err != nil {
		t.Fatalf("RaiseFDLimit() error = %v", err)
	}

	if !limits.Supported {
		t.Fatal("RaiseFDLimit().Supported = false, want true on darwin")
	}

	if limits.Soft <= 0 {
		t.Fatalf("RaiseFDLimit().Soft = %d, want a positive starting soft limit", limits.Soft)
	}

	if limits.Hard <= 0 {
		t.Fatalf("RaiseFDLimit().Hard = %d, want a positive resolved hard ceiling (never literal RLIM_INFINITY)", limits.Hard)
	}

	if limits.Raised < limits.Soft {
		t.Fatalf("RaiseFDLimit().Raised = %d, want >= original Soft %d", limits.Raised, limits.Soft)
	}

	if limits.Raised > limits.Hard {
		t.Fatalf("RaiseFDLimit().Raised = %d, want <= resolved Hard %d", limits.Raised, limits.Hard)
	}
}

func TestRaiseFDLimitIdempotent(t *testing.T) {
	first, err := RaiseFDLimit()
	if err != nil {
		t.Fatalf("first RaiseFDLimit() error = %v", err)
	}

	second, err := RaiseFDLimit()
	if err != nil {
		t.Fatalf("second RaiseFDLimit() error = %v", err)
	}

	if second.Soft != first.Raised {
		t.Fatalf("second call's Soft = %d, want it to observe the first call's Raised value %d", second.Soft, first.Raised)
	}
}

// TestRaiseFDLimitRaisesWhenSoftIsBelowHardCeiling exercises the raise
// branch that a real invocation can never demonstrate: since Go 1.19, the
// Go runtime itself raises RLIMIT_NOFILE's soft limit to the hard ceiling
// before main() runs (src/syscall/rlimit.go), so a genuine Getrlimit call
// in this process always observes Cur already equal to Max and the
// Setrlimit branch never fires. This test substitutes getrlimitFunc and
// setrlimitFunc (the seam RaiseFDLimit calls through) with a mocked starting
// rlimit whose soft limit is genuinely below its hard ceiling, and asserts
// a real Setrlimit call happens with the expected target and that the
// reported original/raised values are exactly the mocked ones.
func TestRaiseFDLimitRaisesWhenSoftIsBelowHardCeiling(t *testing.T) {
	origGetrlimit, origSetrlimit := getrlimitFunc, setrlimitFunc
	t.Cleanup(func() {
		getrlimitFunc = origGetrlimit
		setrlimitFunc = origSetrlimit
	})

	getrlimitFunc = func(which int, lim *unix.Rlimit) error {
		if which != unix.RLIMIT_NOFILE {
			t.Fatalf("Getrlimit called with resource %d, want RLIMIT_NOFILE", which)
		}
		*lim = unix.Rlimit{Cur: 1024, Max: 65536}
		return nil
	}

	var setCalled bool
	var gotResource int
	var gotLim unix.Rlimit
	setrlimitFunc = func(resource int, rlim *unix.Rlimit) error {
		setCalled = true
		gotResource = resource
		gotLim = *rlim
		return nil
	}

	limits, err := RaiseFDLimit()
	if err != nil {
		t.Fatalf("RaiseFDLimit() error = %v", err)
	}

	if !setCalled {
		t.Fatal("RaiseFDLimit() did not call Setrlimit even though the mocked Cur (1024) < Max (65536)")
	}

	if gotResource != unix.RLIMIT_NOFILE {
		t.Fatalf("Setrlimit called with resource %d, want RLIMIT_NOFILE", gotResource)
	}

	if gotLim.Cur != 65536 || gotLim.Max != 65536 {
		t.Fatalf("Setrlimit called with %+v, want Cur=Max=65536", gotLim)
	}

	if limits.Soft != 1024 {
		t.Fatalf("RaiseFDLimit().Soft = %d, want the mocked original 1024", limits.Soft)
	}

	if limits.Hard != 65536 {
		t.Fatalf("RaiseFDLimit().Hard = %d, want the mocked hard ceiling 65536", limits.Hard)
	}

	if limits.Raised != 65536 {
		t.Fatalf("RaiseFDLimit().Raised = %d, want the mocked raised value 65536", limits.Raised)
	}
}
