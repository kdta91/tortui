package platform

import "testing"

func TestTerminalSizeNonTTYErrors(t *testing.T) {
	f, err := newRegularFile(t)
	if err != nil {
		t.Fatalf("newRegularFile: %v", err)
	}

	if _, _, err := TerminalSize(f); err == nil {
		t.Fatal("TerminalSize(regular file) error = nil, want an error — a plain file is never a terminal")
	}
}
