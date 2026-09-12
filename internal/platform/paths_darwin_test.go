package platform

import (
	"path/filepath"
	"testing"
)

func TestConfigDirDarwin(t *testing.T) {
	tests := []struct {
		name          string
		home          string
		xdgConfigHome string
		want          string
	}{
		{
			name: "default under HOME/.config",
			home: "/Users/alice",
			want: "/Users/alice/.config/tortui",
		},
		{
			name:          "XDG_CONFIG_HOME override",
			home:          "/Users/alice",
			xdgConfigHome: "/Users/alice/.xdgconfig",
			want:          "/Users/alice/.xdgconfig/tortui",
		},
		{
			name:          "relative XDG_CONFIG_HOME is ignored",
			home:          "/Users/alice",
			xdgConfigHome: "relative/path",
			want:          "/Users/alice/.config/tortui",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.home)
			t.Setenv("XDG_CONFIG_HOME", tt.xdgConfigHome)

			got, err := ConfigDir()
			if err != nil {
				t.Fatalf("ConfigDir() error = %v", err)
			}

			if got != filepath.Clean(tt.want) {
				t.Fatalf("ConfigDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStateDirDarwinMatchesConfigDir(t *testing.T) {
	t.Setenv("HOME", "/Users/alice")
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}

	state, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir() error = %v", err)
	}

	if state != cfg {
		t.Fatalf("StateDir() = %q, want it to match ConfigDir() = %q on macOS", state, cfg)
	}
}

func TestDownloadDirDarwin(t *testing.T) {
	t.Setenv("HOME", "/Users/alice")
	// macOS does not honor $XDG_DOWNLOAD_DIR (unlike Linux); confirm it is
	// ignored rather than silently changing the resolved path.
	t.Setenv("XDG_DOWNLOAD_DIR", "/Volumes/External/dl")

	got, err := DownloadDir()
	if err != nil {
		t.Fatalf("DownloadDir() error = %v", err)
	}

	want := filepath.Clean("/Users/alice/Downloads/tortui")
	if got != want {
		t.Fatalf("DownloadDir() = %q, want %q", got, want)
	}
}
