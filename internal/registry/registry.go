package registry

// transitions is the only permitted lifecycle graph. Any edge not listed is rejected.
var transitions = map[string]map[string]bool{
	"quarantined": {"verified": true},
	"verified":    {"approved": true},
	"approved":    {"installed": true},
	"installed":   {"active": true},
	"active":      {"draining": true, "revoked": true},
	"draining":    {"disabled": true},
	"disabled":    {"active": true},
	"revoked":     {},
}

// CanTransition reports whether the lifecycle edge exists.
func CanTransition(from, to string) bool { return transitions[from][to] }

// CanTransitionAs gates who may move a release. Verification, approval, installation,
// activation, draining, disabling and revocation are operator (admin) actions: a release
// author cannot verify or activate their own artefact. Submission happens at create time
// in the quarantined state, so this function is never the create path.
func CanTransitionAs(role, target string) bool {
	if target == "" {
		return false
	}
	return role == "admin"
}
