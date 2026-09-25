package platform

import "testing"

func TestPathLengthCountsBytesOnDarwin(t *testing.T) {
	t.Parallel()

	if got := PathLength("名"); got != 3 {
		t.Fatalf("PathLength = %d, want 3 bytes", got)
	}
}
