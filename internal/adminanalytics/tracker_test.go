package adminanalytics

import "testing"

func TestNormalizeRole(t *testing.T) {
	tests := map[string]string{
		"DRIVER":         RoleDriver,
		"CARGO_MANAGER":  RoleCargoManager,
		"DRIVER_MANAGER": RoleDriverManager,
		"ADMIN":          RoleAdmin,
		"dispatcher":     "dispatcher",
	}
	for input, want := range tests {
		if got := NormalizeRole(input); got != want {
			t.Fatalf("NormalizeRole(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHashIPDeterministic(t *testing.T) {
	a := HashIP("127.0.0.1", "salt")
	b := HashIP("127.0.0.1", "salt")
	c := HashIP("127.0.0.1", "salt-2")
	if a == "" {
		t.Fatal("expected non-empty hash")
	}
	if a != b {
		t.Fatal("expected hash to be deterministic")
	}
	if a == c {
		t.Fatal("expected different salt to produce different hash")
	}
}
