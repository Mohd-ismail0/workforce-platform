package testdb

import "testing"

func TestRewriteDatabaseSwapsPath(t *testing.T) {
	cases := []struct {
		in, db, want string
	}{
		// Plain password, no special chars: path swaps cleanly.
		{"postgres://user:pass@host:5432/old", "new", "postgres://user:pass@host:5432/new"},
		// Query (options) survives the swap.
		{"postgres://user:pass@host/db?sslmode=require", "fresh", "postgres://user:pass@host/fresh?sslmode=require"},
		// Fragment is dropped (never meaningful in a DSN).
		{"postgres://user:pass@host/db#frag", "x", "postgres://user:pass@host/x"},
	}
	for _, c := range cases {
		if got := rewriteDatabase(c.in, c.db); got != c.want {
			t.Errorf("rewriteDatabase(%q,%q)=%q want %q", c.in, c.db, got, c.want)
		}
	}
}

// A password with characters that url.Parse percent-encodes must still swap the
// path without corrupting the credential. Assert on the parsed components rather
// than a literal re-serialization, because net/url re-encodes delimiters.
func TestRewriteDatabasePreservesEncodedCredential(t *testing.T) {
	in := "postgres://user:abc%2Fdef@host:5432/old"
	got := rewriteDatabase(in, "new")
	if got == in {
		t.Fatalf("path not swapped: %q", got)
	}
	for _, wantToken := range []string{"@host:5432/new", "abc%2Fdef"} {
		var found bool
		for i := 0; i+len(wantToken) <= len(got); i++ {
			if got[i:i+len(wantToken)] == wantToken {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("rewriteDatabase(%q) missing %q -> %q", in, wantToken, got)
		}
	}
}

func TestDbNameStableAndShort(t *testing.T) {
	a := dbName("api")
	b := dbName("api")
	if a != b {
		t.Fatalf("dbName not stable: %s vs %s", a, b)
	}
	if a == dbName("store") {
		t.Fatalf("dbName collision between packages")
	}
	if len(a) > 63 {
		t.Fatalf("dbName too long: %d (%s)", len(a), a)
	}
}