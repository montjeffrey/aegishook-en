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

// Server text is shown to the user and the agent, so it must not carry terminal
// escape sequences or bidi controls, and it is length-capped.
func TestPrintableStripsControlCharacters(t *testing.T) {
	esc := string(rune(0x1b))
	got := printable("ok" + esc + "[2J" + esc + "]0;owned\u0007 line1\r\nline2\u202eevil\u200b\u0085")
	if strings.ContainsAny(got, esc+"\r\u0007\u202e\u200b\u0085") || strings.Contains(got, "\n") {
		t.Fatalf("control characters survived: %q", got)
	}
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Fatalf("text lost: %q", got)
	}
	if long := printable(strings.Repeat("x", 5000)); len([]rune(long)) > 510 {
		t.Fatalf("not capped: %d", len(long))
	}
}
