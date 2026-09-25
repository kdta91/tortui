package platform

import "testing"

func TestPathLengthCountsUTF16UnitsOnWindows(t *testing.T) {
	t.Parallel()

	// "名" is 3 UTF-8 bytes but one UTF-16 unit; "😀" is two units.
	if got := PathLength("名😀"); got != 3 {
		t.Fatalf("PathLength = %d, want 3 UTF-16 units", got)
	}

	if MaxPathLength != 259 {
		t.Fatalf("MaxPathLength = %d, want 259 (MAX_PATH less NUL)", MaxPathLength)
	}
}
