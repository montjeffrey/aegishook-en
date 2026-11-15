package core

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Plain static commands that name a protected path with shell quoting, escapes,
// globs or a directory change must be rejected like the plain spelling.
func TestSelfProtectRejectsQuotingGlobsAndDirectories(t *testing.T) {
	isolatedHome(t)
	e := engine(t)
	// The probe cases below use the default data directory name; a glob against the
	// engine's own temporary directory is added when the platform allows it.
	cmds := []string{
		"cat ~/.aegis''hook/admin.token",
		`cat ~/.aegis\hook/admin.token`,
		`cat ~/.aegis"hook"/admin.token`,
		"cat ~/.claude/settings.j''son",
		`cat ~/.claude/settings.js\on`,
		"cat ~/.aeg*/admin.token",
		"cat ~/.aegishoo?/admin.token",
		"cat ~/.aegishoo[k]/admin.token",
		"cat ~/.claude/settings.*",
		"cd ~/.claude && echo {} > settings.json",
		"mv ~/.claude ~/.claude.bak",
		"mv ~/.codex ~/.codex.bak",
		"rm -rf ~/.claude/",
		"mv ~/.grok/hooks /tmp/h",
	}
	for _, cmd := range cmds {
		if r := submitRejected(t, e, "Bash", map[string]any{"command": cmd}); r.RuleID != selfProtectRule {
			t.Errorf("%q: got %s / %s", cmd, r.Decision, r.RuleID)
		}
	}
	// A glob that would match the data directory of this engine.
	glob := filepath.ToSlash(filepath.Join(filepath.Dir(e.Dir), "*", "admin.token"))
	if r := submitRejected(t, e, "Bash", map[string]any{"command": "cat " + glob}); r.RuleID != selfProtectRule {
		t.Errorf("glob %q: got %s / %s", glob, r.Decision, r.RuleID)
	}
}

func TestSelfProtectDirectoryIsExactNotPrefix(t *testing.T) {
	isolatedHome(t)
	e := engine(t)
	// Other files in the agent config directory stay usable.
	for _, cmd := range []string{"cat ~/.claude/CLAUDE.md", "ls ~/.claude/projects", "cat ~/.claude.json"} {
		if r := submitRejected(t, e, "Bash", map[string]any{"command": cmd}); r.RuleID == selfProtectRule {
			t.Errorf("%q wrongly rejected by self-protection", cmd)
		}
	}
}

func TestSelfProtectGlobFollowsShellDotfileRule(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows shells match dot-files with *")
	}
	isolatedHome(t)
	e := engine(t)
	// In a POSIX shell * does not match .aegishook, so this is not a path to it.
	if r := submitRejected(t, e, "Bash", map[string]any{"command": "ls ~/*"}); r.RuleID == selfProtectRule {
		t.Errorf("~/* rejected: %s", r.RuleID)
	}
}

// Padding the call with many path-like tokens must not push the real target past
// the lookup budget, and exhausting the budget rejects instead of allowing.
func TestSelfProtectPaddingAndBudget(t *testing.T) {
	isolatedHome(t)
	e := engine(t)
	pad := strings.Repeat("a/ ", 520)
	for _, tail := range []string{"cat ~/.claude//settings.json", "cat ~/.claude/./settings.json", "cat ~/x/../.claude/settings.json"} {
		if r := submitRejected(t, e, "Bash", map[string]any{"command": pad + "; " + tail}); r.RuleID != selfProtectRule {
			t.Errorf("padded %q: got %s / %s", tail, r.Decision, r.RuleID)
		}
	}
	var many strings.Builder
	for i := 0; i < maxSelfResolve+50; i++ {
		many.WriteString("p")
		many.WriteString(strings.Repeat("x", i%7))
		many.WriteString(string(rune('a' + i%26)))
		many.WriteString(string(rune('a' + (i/26)%26)))
		many.WriteString("/f ")
	}
	r := submitRejected(t, e, "Bash", map[string]any{"command": "echo " + many.String()})
	if r.Decision != "reject" || r.RuleID != selfProtectRule {
		t.Errorf("budget exhaustion must fail closed, got %s / %s", r.Decision, r.RuleID)
	}
}

// If the installation records cannot be read the protected set may be incomplete,
// so the call is refused.
func TestSelfProtectFailsClosedWhenInstallationsUnreadable(t *testing.T) {
	isolatedHome(t)
	e := engine(t)
	if _, err := e.DB.Exec(`INSERT INTO records(kind,id,payload) VALUES('installations','bad','{"id":5}')`); err != nil {
		t.Fatal(err)
	}
	r := submitRejected(t, e, "Bash", map[string]any{"command": "ls"})
	if r.Decision != "reject" || r.RuleID != selfProtectRule {
		t.Fatalf("got %s / %s", r.Decision, r.RuleID)
	}
}

// What a static check cannot see is pinned here so nobody mistakes it for covered.
func TestSelfProtectKnownLimits(t *testing.T) {
	isolatedHome(t)
	e := engine(t)
	for _, cmd := range []string{
		"d=.aegis; cat ~/${d}hook/admin.token", // path built at run time
		"grep -r token ~",                      // whole-home recursive read
		"tar czf /tmp/h.tgz ~",
	} {
		if r := submitRejected(t, e, "Bash", map[string]any{"command": cmd}); r.RuleID == selfProtectRule {
			t.Errorf("%q is now caught; update the documented limits", cmd)
		}
	}
}

func TestExpandSelfEnvXDGConfigHome(t *testing.T) {
	home := isolatedHome(t)
	if got := expandSelfEnv("$XDG_CONFIG_HOME/opencode/plugins/aegishook.js"); got != filepath.Join(home, ".config")+"/opencode/plugins/aegishook.js" {
		t.Errorf("unset XDG_CONFIG_HOME: %q", got)
	}
	cfg := filepath.Join(home, "cfg")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	if got := expandSelfEnv("${XDG_CONFIG_HOME}/opencode/plugins/aegishook.js"); got != cfg+"/opencode/plugins/aegishook.js" {
		t.Errorf("set XDG_CONFIG_HOME: %q", got)
	}
}
