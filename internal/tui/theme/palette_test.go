package theme

import "testing"

func TestNamesHasAtLeastTwoBuiltinThemes(t *testing.T) {
	names := Names()
	if len(names) < 2 {
		t.Fatalf("Names() = %v, want at least 2 built-in themes", names)
	}

	for _, name := range names {
		if _, ok := Lookup(name); !ok {
			t.Errorf("Lookup(%q) reported not-ok for a name Names() returned", name)
		}
	}
}

func TestLookupUnknownName(t *testing.T) {
	if _, ok := Lookup("does-not-exist"); ok {
		t.Fatal("Lookup(\"does-not-exist\") = ok, want not-ok")
	}
}

func TestDefaultThemeNameIsLookupable(t *testing.T) {
	if _, ok := Lookup(DefaultThemeName); !ok {
		t.Fatalf("Lookup(DefaultThemeName=%q) = not-ok, want ok", DefaultThemeName)
	}
}

func TestNamesAreSorted(t *testing.T) {
	names := Names()
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("Names() = %v, not sorted", names)
		}
	}
}
