package engine

import "testing"

func TestStateString(t *testing.T) {
	cases := []struct {
		state State
		want  string
	}{
		{StateQueued, "queued"},
		{StateChecking, "checking"},
		{StateDownloading, "downloading"},
		{StateSeeding, "seeding"},
		{StatePaused, "paused"},
		{StateErrored, "errored"},
		{State(99), "state(99)"},
	}

	for _, tc := range cases {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", int(tc.state), got, tc.want)
		}
	}
}

func TestStateQueuedIsZeroValue(t *testing.T) {
	var s State
	if s != StateQueued {
		t.Errorf("zero value of State = %v, want StateQueued", s)
	}
}
