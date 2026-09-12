package platform

import (
	"path/filepath"
	"testing"
)

func TestConfigDirLinux(t *testing.T) {
	tests := []struct {
		name          string
		home          string
		xdgConfigHome string
		want          string
	}{
		{
			name: "default under HOME/.config",
			home: "/home/alice",
			want: "/home/alice/.config/tortui",
		},
		{
			name:          "XDG_CONFIG_HOME override",
			home:          "/home/alice",
			xdgConfigHome: "/home/alice/.xdgconfig",
			want:          "/home/alice/.xdgconfig/tortui",
		},
		{
			name:          "relative XDG_CONFIG_HOME is ignored",
			home:          "/home/alice",
			xdgConfigHome: "relative/path",
			want:          "/home/alice/.config/tortui",
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

func TestStateDirLinux(t *testing.T) {
	tests := []struct {
		name         string
		home         string
		xdgStateHome string
		want         string
	}{
		{
			name: "default under HOME/.local/state",
			home: "/home/alice",
			want: "/home/alice/.local/state/tortui",
		},
		{
			name:         "XDG_STATE_HOME override",
			home:         "/home/alice",
			xdgStateHome: "/home/alice/.xdgstate",
			want:         "/home/alice/.xdgstate/tortui",
		},
		{
			name:         "relative XDG_STATE_HOME is ignored",
			home:         "/home/alice",
			xdgStateHome: "relative/path",
			want:         "/home/alice/.local/state/tortui",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.home)
			t.Setenv("XDG_STATE_HOME", tt.xdgStateHome)

			got, err := StateDir()
			if err != nil {
				t.Fatalf("StateDir() error = %v", err)
			}

			if got != filepath.Clean(tt.want) {
				t.Fatalf("StateDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDownloadDirLinux(t *testing.T) {
	tests := []struct {
		name           string
		home           string
		xdgDownloadDir string
		want           string
	}{
		{
			name: "default under HOME/Downloads",
			home: "/home/alice",
			want: "/home/alice/Downloads/tortui",
		},
		{
			name:           "XDG_DOWNLOAD_DIR override",
			home:           "/home/alice",
			xdgDownloadDir: "/mnt/external/dl",
			want:           "/mnt/external/dl/tortui",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.home)
			t.Setenv("XDG_DOWNLOAD_DIR", tt.xdgDownloadDir)

			got, err := DownloadDir()
			if err != nil {
				t.Fatalf("DownloadDir() error = %v", err)
			}

			if got != filepath.Clean(tt.want) {
				t.Fatalf("DownloadDir() = %q, want %q", got, tt.want)
			}
		})
	}
}
