package registry

import "testing"

func TestTransitionGraphIsStrict(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{"quarantined", "verified", true},
		{"quarantined", "active", false},
		{"quarantined", "approved", false},
		{"verified", "installed", false},
		{"approved", "installed", true},
		{"installed", "active", true},
		{"active", "revoked", true},
		{"active", "verified", false},
		{"revoked", "active", false},
		{"disabled", "active", true},
		{"draining", "disabled", true},
		{"unknown", "verified", false},
	}
	for _, tc := range cases {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Fatalf("CanTransition(%s,%s)=%v want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestAuthorCannotSelfVerifyOrActivate(t *testing.T) {
	for _, role := range []string{"requester", "approver", ""} {
		if CanTransitionAs(role, "verified") {
			t.Fatalf("role %q may self-verify", role)
		}
		if CanTransitionAs(role, "active") {
			t.Fatalf("role %q may activate", role)
		}
	}
	if !CanTransitionAs("admin", "verified") || !CanTransitionAs("admin", "revoked") {
		t.Fatal("admin must be able to verify and revoke")
	}
	if CanTransitionAs("admin", "") {
		t.Fatal("empty target must be rejected")
	}
}
