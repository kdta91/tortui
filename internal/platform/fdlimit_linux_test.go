package platform

import "testing"

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
