package platform

import "testing"

func TestRaiseFDLimitReportsUnsupported(t *testing.T) {
	limits, err := RaiseFDLimit()
	if err != nil {
		t.Fatalf("RaiseFDLimit() error = %v", err)
	}

	if limits.Supported {
		t.Fatal("RaiseFDLimit().Supported = true, want false on windows")
	}

	if limits.Soft != -1 || limits.Hard != -1 || limits.Raised != -1 {
		t.Fatalf("RaiseFDLimit() = %+v, want all fields -1 on windows", limits)
	}
}
