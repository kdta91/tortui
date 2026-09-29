package indexer

import "testing"

func TestMeetsMinSeeders(t *testing.T) {
	t.Parallel()

	unknown := map[string]string{ExtraKeySeedersUnknown: "1"}

	tests := []struct {
		name string
		res  Result
		min  int
		want bool
	}{
		{"no minimum keeps a zero", Result{Seeders: 0}, 0, true},
		{"unknown passes a minimum", Result{Extra: unknown}, 5, true},
		{"known below minimum is dropped", Result{Seeders: 4}, 5, false},
		{"known real zero is dropped", Result{Seeders: 0}, 1, false},
		{"known at minimum is kept", Result{Seeders: 5}, 5, true},
		{"known above minimum is kept", Result{Seeders: 6}, 5, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := MeetsMinSeeders(tc.res, tc.min); got != tc.want {
				t.Fatalf("MeetsMinSeeders(%+v, %d) = %v, want %v", tc.res, tc.min, got, tc.want)
			}
		})
	}
}
