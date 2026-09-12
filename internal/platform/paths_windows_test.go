package platform

import (
	"path/filepath"
	"testing"
)

func TestConfigDirWindows(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\alice\AppData\Roaming`)

	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}

	want := filepath.Clean(`C:\Users\alice\AppData\Roaming\tortui`)
	if got != want {
		t.Fatalf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestConfigDirWindowsMissingAppData(t *testing.T) {
	t.Setenv("APPDATA", "")

	if _, err := ConfigDir(); err == nil {
		t.Fatal("ConfigDir() error = nil, want an error when %AppData% is unset")
	}
}

func TestStateDirWindows(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\alice\AppData\Local`)

	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir() error = %v", err)
	}

	want := filepath.Clean(`C:\Users\alice\AppData\Local\tortui`)
	if got != want {
		t.Fatalf("StateDir() = %q, want %q", got, want)
	}
}

func TestDownloadDirWindows(t *testing.T) {
	t.Setenv("USERPROFILE", `C:\Users\alice`)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")

	got, err := DownloadDir()
	if err != nil {
		t.Fatalf("DownloadDir() error = %v", err)
	}

	want := filepath.Clean(`C:\Users\alice\Downloads\tortui`)
	if got != want {
		t.Fatalf("DownloadDir() = %q, want %q", got, want)
	}
}
