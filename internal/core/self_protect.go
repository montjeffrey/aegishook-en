package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Fixed self-protection. These checks run before scopes, rules and any reviewer,
// are not rules, and cannot be disabled or overridden by an allow rule. The agent
// under review must not read the gate's credentials (admin.token, model.key,
// connection.json) or change the hook entries and adapter files that connect it.
//
// The check is static: it looks at every string in the call (arguments and script
// snapshots), so it catches paths named in plain text, with ~ or common
// environment-variable forms, relative paths, symlinks and case changes. It cannot
// see a path that a shell builds at run time (for example from a variable or a
// glob); such a call still reaches the configured reviewer.

// selfMarkers name the default data and client directories wherever they appear.
var selfMarkers = []string{".aegishook"}

// selfSuffixes are the hook entries the installers write, relative to any base.
var selfSuffixes = []string{
	".claude/settings.json",
	".claude/settings.local.json",
	".codex/hooks.json",
	".grok/hooks/aegishook.json",
	"plugins/aegishook.js",
	"extensions/aegishook.ts",
}

const selfProtectRule = "SELF_PROTECT"

var (
	selfSplit = regexp.MustCompile("[\\s;|&<>()\"'`=,]+")
	selfEnv   = regexp.MustCompile(`(?i)\$\{env:(\w+)\}|\$env:(\w+)|\$\{(\w+)\}|\$(\w+)|%(\w+)%`)
)

// maxSelfResolve bounds the filesystem lookups made per call; the textual checks
// are not bounded.
const maxSelfResolve = 512

func normalizeSelf(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "\\", "/"))
}

// selfProtectedPaths lists what the gate protects on this machine. Caller holds e.mu.
func (e *Engine) selfProtectedPaths() []string {
	roots := []string{e.Dir}
	if e.IntegrationBinary != "" {
		roots = append(roots, e.IntegrationBinary)
	}
	for _, agent := range []string{"claude", "codex", "grok", "opencode", "pi"} {
		base := agentHome(agent)
		switch agent {
		case "claude":
			roots = append(roots, filepath.Join(base, "settings.json"), filepath.Join(base, "settings.local.json"))
		case "codex":
			roots = append(roots, filepath.Join(base, "hooks.json"))
		case "grok":
			roots = append(roots, filepath.Join(base, "hooks", "aegishook.json"))
		case "opencode":
			roots = append(roots, filepath.Join(base, "plugins", "aegishook.js"))
		default:
			roots = append(roots, filepath.Join(base, "extensions", "aegishook.ts"))
		}
	}
	if installs, err := list[Installation](e, "installations"); err == nil {
		for _, i := range installs {
			if i.Entry != "" {
				roots = append(roots, i.Entry)
			}
		}
	}
	return roots
}

// selfProtect returns a reject decision when the call names a protected path.
// local is false for a remote node, whose home and environment are unknown here.
func (e *Engine) selfProtect(in ReviewInput, cwd string, resolve func(string, string) string, local bool) *Decision {
	roots := e.selfProtectedPaths()
	if selfTouches(in, cwd, roots, resolve, local) {
		return &Decision{Decision: "reject", RuleID: selfProtectRule, Comment: "Actual operation: access the gate's own data directory, credentials or hook configuration; Consequence if successful: the agent could read the gate's tokens and keys or switch off the gate; Matched rule: " + selfProtectRule + " (fixed self-protection, cannot be disabled). Do not touch AegisHook files; ask the user to change them."}
	}
	return nil
}

func selfTouches(in ReviewInput, cwd string, roots []string, resolve func(string, string) string, local bool) bool {
	normRoots := make([]string, 0, len(roots))
	for _, r := range roots {
		if r != "" {
			normRoots = append(normRoots, strings.TrimRight(normalizeSelf(r), "/"))
		}
	}
	resolved := 0
	check := func(text string) bool {
		whole := normalizeSelf(text)
		if matchesSelf(whole, normRoots, false) {
			return true
		}
		for _, tok := range selfSplit.Split(text, -1) {
			if tok == "" {
				continue
			}
			if local {
				tok = expandSelfEnv(tok)
			}
			n := normalizeSelf(tok)
			if matchesSelf(n, normRoots, true) {
				return true
			}
			if resolved < maxSelfResolve && strings.ContainsAny(tok, "/\\.~") {
				resolved++
				if r := resolve(tok, cwd); r != "" && matchesSelf(normalizeSelf(r), normRoots, true) {
					return true
				}
			}
		}
		return false
	}
	var walk func(v any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case string:
			return check(x)
		case map[string]any:
			for _, e := range x {
				if walk(e) {
					return true
				}
			}
		case []any:
			for _, e := range x {
				if walk(e) {
					return true
				}
			}
		}
		return false
	}
	if walk(in.Arguments) {
		return true
	}
	for _, s := range in.Scripts {
		if check(s.Path) || check(s.Content) {
			return true
		}
	}
	return false
}

// matchesSelf reports whether n (lower-case, forward slashes) names a protected
// location. asPath also accepts exact and suffix matches, which only make sense for
// a single path-like token.
func matchesSelf(n string, roots []string, asPath bool) bool {
	for _, m := range selfMarkers {
		if strings.Contains(n, m) {
			return true
		}
	}
	for _, r := range roots {
		if asPath {
			if n == r || strings.HasPrefix(n, r+"/") {
				return true
			}
		} else if containsPath(n, r) {
			return true
		}
	}
	if asPath {
		for _, s := range selfSuffixes {
			if n == s || strings.HasSuffix(n, "/"+s) {
				return true
			}
		}
	}
	return false
}

// containsPath reports whether root appears in text as a whole path, not as part
// of a longer name (so /data does not match /database or /home/u/data).
func containsPath(text, root string) bool {
	if root == "" {
		return false
	}
	for from := 0; ; {
		i := strings.Index(text[from:], root)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(root)
		if (start == 0 || !pathChar(text[start-1])) && (end == len(text) || !pathChar(text[end]) || text[end] == '/') {
			return true
		}
		from = start + 1
	}
}
func pathChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("._~/-", c) >= 0
}

// expandSelfEnv replaces ~ and the home and agent-config variables a shell would
// expand, in POSIX ($HOME, ${HOME}), cmd (%USERPROFILE%) and PowerShell
// ($env:USERPROFILE) forms.
func expandSelfEnv(tok string) string {
	home, _ := os.UserHomeDir()
	if tok == "~" || strings.HasPrefix(tok, "~/") || strings.HasPrefix(tok, `~\`) {
		tok = home + tok[1:]
	}
	return selfEnv.ReplaceAllStringFunc(tok, func(m string) string {
		sub := selfEnv.FindStringSubmatch(m)
		name := ""
		for _, s := range sub[1:] {
			if s != "" {
				name = s
			}
		}
		switch strings.ToLower(name) {
		case "home", "userprofile":
			return home
		case "claude_config_dir":
			return agentHome("claude")
		case "codex_home":
			return agentHome("codex")
		case "opencode_config_dir", "xdg_config_home":
			return agentHome("opencode")
		}
		return m
	})
}
