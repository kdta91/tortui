package platform

import (
	"os"
	"os/exec"
	"testing"
)

func TestProcessAliveSelf(t *testing.T) {
	alive, err := ProcessAlive(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessAlive(self) error = %v", err)
	}
	if !alive {
		t.Fatal("ProcessAlive(self) = false, want true — this process is plainly running")
	}
}

func TestProcessAliveExitedProcess(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run helper process: %v", err)
	}
	pid := cmd.Process.Pid

	alive, err := ProcessAlive(pid)
	if err != nil {
		t.Fatalf("ProcessAlive(exited) error = %v", err)
	}
	if alive {
		t.Fatalf("ProcessAlive(%d) = true, want false for a process that has already exited", pid)
	}
}

func TestProcessAliveNonPositivePID(t *testing.T) {
	alive, err := ProcessAlive(0)
	if err != nil {
		t.Fatalf("ProcessAlive(0) error = %v", err)
	}
	if alive {
		t.Fatal("ProcessAlive(0) = true, want false")
	}

	alive, err = ProcessAlive(-1)
	if err != nil {
		t.Fatalf("ProcessAlive(-1) error = %v", err)
	}
	if alive {
		t.Fatal("ProcessAlive(-1) = true, want false")
	}
}
