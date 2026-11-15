package core

import "testing"

// These calls look ordinary but trip the fixed checks. The test pins the CURRENT
// behaviour so the owner can see exactly what is rejected or sent to a human; it is
// documentation of a trade-off, not a statement that the outcome is ideal. Loosening
// a rule should change this table on purpose.
func TestOrdinaryLookingCallsAndTheirCurrentOutcome(t *testing.T) {
	isolatedHome(t)
	e := engine(t)
	for _, c := range []struct {
		name, tool string
		args       map[string]any
		decision   string
		rule       string
		hidden     int
	}{
		{"edit README text that mentions ~/.aegishook", "Edit", map[string]any{"file_path": "README.md", "old_string": "see ~/.aegishook", "new_string": "see the data dir"}, "reject", selfProtectRule, 0},
		{"git add a project's own .claude/settings.json", "Bash", map[string]any{"command": "git add .claude/settings.json"}, "reject", selfProtectRule, 0},
		{"read docs/.claude/settings.json", "Read", map[string]any{"file_path": "docs/.claude/settings.json"}, "reject", selfProtectRule, 0},
		{"code that declares password: z.string()", "Write", map[string]any{"file_path": "schema.ts", "content": "const s = { password: z.string() };"}, "reject", secretSyntaxRule, 1},
		{"grep -r password= .", "Bash", map[string]any{"command": `grep -r "password=" .`}, "pending", "", 1},
		{"plain password assignment in a config file", "Write", map[string]any{"file_path": "app.env", "content": "password=hunter2\n"}, "pending", "", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := submitRejected(t, e, c.tool, c.args)
			if r.Decision != c.decision || r.RuleID != c.rule || r.RedactedSpans != c.hidden {
				t.Fatalf("got %s / %q / %d hidden spans, want %s / %q / %d", r.Decision, r.RuleID, r.RedactedSpans, c.decision, c.rule, c.hidden)
			}
			if c.decision == "pending" && c.hidden > 0 && (!r.NeedsHuman || r.ReviewPath != "human") {
				t.Fatalf("hidden spans must go to a human: %+v", r)
			}
		})
	}
}

// Near misses that are NOT touched by the fixed checks: a project-level .claude
// directory listing and a flag that merely looks like a password option.
func TestNearMissesAreNotCaughtByFixedChecks(t *testing.T) {
	isolatedHome(t)
	e := engine(t)
	for _, cmd := range []string{"ls .claude", "mkdir -p build/out", "ssh -p 22 host true"} {
		r := submitRejected(t, e, "Bash", map[string]any{"command": cmd})
		if r.RuleID == selfProtectRule || r.RuleID == secretSyntaxRule || r.RedactedSpans != 0 {
			t.Errorf("%q: got %s / %q / %d hidden spans", cmd, r.Decision, r.RuleID, r.RedactedSpans)
		}
	}
}
