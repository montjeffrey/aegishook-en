package bridge

import (
	"strings"
	"testing"
)

// Reusing a tool call ID with different arguments is refused, and the hook says why.
func TestArgumentTamperDenialExplainsTheReason(t *testing.T) {
	_, path, cwd := fixture(t)
	if code, _, log := invoke("claude", path, native("claude", cwd, "same-id", "Bash", "PreToolUse", map[string]any{"command": "passwd fixture"})); code != 2 {
		t.Fatal(code, log)
	}
	code, out, log := invoke("claude", path, native("claude", cwd, "same-id", "Bash", "PreToolUse", map[string]any{"command": "passwd other"}))
	if code != 2 || !strings.Contains(log, "HTTP 409") || !strings.Contains(log, "already used with different arguments") || !strings.Contains(out, "already used with different arguments") {
		t.Fatalf("denial does not explain the reason: code=%d out=%q log=%q", code, out, log)
	}
}
