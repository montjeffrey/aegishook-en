package core

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Fixed self-protection. These checks run before scopes, rules and any reviewer,
// are not rules, and cannot be disabled or overridden by an allow rule. The agent
// under review must not read the gate's credentials (admin.token, model.key,
// connection.json) or change the hook entries and adapter files that connect it.
//
// This is defence in depth and best effort, not a security boundary. The check is
// static: it looks at every string in the call (arguments and script snapshots), so
// it catches paths named in plain text (also with quotes or backslash escapes
// spliced in), with ~ or common environment-variable forms, simple globs, relative
// paths, symlinks and case changes. It cannot see a path that a shell builds at run
// time (a variable, command substitution, brace expansion), a whole-home recursive
// read (grep -r token ~), or a Windows junction (junctions are not followed; only
// symlinks the platform resolves are). Such a call still reaches the configured
// reviewer. An agent running as the same OS user as the gate can always read the
// gate's files; see the README for the stronger options.

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
	// selfStrip removes the characters a shell drops while parsing a word, so that
	// ~/.aegis''hook and ~/.aegis\hook read as ~/.aegishook.
	selfStrip = strings.NewReplacer("'", "", "\"", "", "\\", "", "`", "")
)

// Lookup budgets. Tokens with a path separator are resolved through the filesystem
// (symlinks); once that budget is spent the call is rejected rather than waved
// through. Bare names are resolved on a best-effort basis and then skipped.
const (
	maxSelfResolve     = 512
	maxSelfBareResolve = 256
)

func normalizeSelf(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "\\", "/"))
}

// cleanSelf lexically cleans a normalised token (no filesystem access) so that
// ~/.claude//settings.json and ~/.claude/./settings.json match like the plain form.
func cleanSelf(n string) string {
	if strings.Contains(n, "/") {
		return path.Clean(n)
	}
	return n
}

// selfSet is what the gate protects on this machine: files and trees (roots, also
// matched as a prefix) and directories that must not be moved or replaced (dirs,
// matched exactly).
type selfSet struct {
	roots, dirs []string
}

// selfProtectedPaths lists what the gate protects on this machine. ok is false when
// the installation records could not be read, so the list may be incomplete.
// Caller holds e.mu. CLAUDE_CONFIG_DIR, CODEX_HOME and the other variables are read
// from the server's environment, which need not be the agent's.
func (e *Engine) selfProtectedPaths() (set selfSet, ok bool) {
	set.roots = []string{e.Dir}
	// The default data and client directories are protected even when this engine
	// uses another data directory, so globs and spellings of them are caught.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		set.roots = append(set.roots, filepath.Join(home, ".aegishook"), filepath.Join(home, ".aegishook-client"))
	}
	if e.IntegrationBinary != "" {
		set.roots = append(set.roots, e.IntegrationBinary)
	}
	for _, agent := range []string{"claude", "codex", "grok", "opencode", "pi"} {
		base := agentHome(agent)
		var files []string
		switch agent {
		case "claude":
			files = []string{filepath.Join(base, "settings.json"), filepath.Join(base, "settings.local.json")}
			set.dirs = append(set.dirs, base)
		case "codex":
			files = []string{filepath.Join(base, "hooks.json")}
			set.dirs = append(set.dirs, base)
		case "grok":
			files = []string{filepath.Join(base, "hooks", "aegishook.json")}
			set.dirs = append(set.dirs, base, filepath.Join(base, "hooks"))
		case "opencode":
			files = []string{filepath.Join(base, "plugins", "aegishook.js")}
			set.dirs = append(set.dirs, filepath.Join(base, "plugins"))
		default:
			files = []string{filepath.Join(base, "extensions", "aegishook.ts")}
			set.dirs = append(set.dirs, filepath.Join(base, "extensions"))
		}
		set.roots = append(set.roots, files...)
	}
	installs, err := list[Installation](e, "installations")
	if err != nil {
		return set, false
	}
	for _, i := range installs {
		if i.Entry != "" {
			set.roots = append(set.roots, i.Entry)
		}
	}
	return set, true
}

// selfProtect returns a reject decision when the call names a protected path.
// local is false for a remote node, whose home and environment are unknown here.
func (e *Engine) selfProtect(in ReviewInput, cwd string, resolve func(string, string) string, local bool) *Decision {
	set, ok := e.selfProtectedPaths()
	if !ok {
		return &Decision{Decision: "reject", RuleID: selfProtectRule, Comment: "Actual operation: not determined; Consequence if successful: unknown; Matched rule: " + selfProtectRule + " (fixed self-protection could not read the installation records, so the call is refused rather than allowed unchecked). Retry once the service is healthy."}
	}
	if selfTouches(in, cwd, set, resolve, local) {
		return &Decision{Decision: "reject", RuleID: selfProtectRule, Comment: "Actual operation: access the gate's own data directory, credentials or hook configuration; Consequence if successful: the agent could read the gate's tokens and keys or switch off the gate; Matched rule: " + selfProtectRule + " (fixed self-protection, cannot be disabled). Do not touch AegisHook files; ask the user to change them."}
	}
	return nil
}

func normSelfList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		if r != "" {
			out = append(out, path.Clean(strings.TrimRight(normalizeSelf(r), "/")))
		}
	}
	return out
}

func selfTouches(in ReviewInput, cwd string, set selfSet, resolve func(string, string) string, local bool) bool {
	roots, dirs := normSelfList(set.roots), normSelfList(set.dirs)
	seen := map[string]bool{}
	pathResolved, bareResolved := 0, 0
	checkOne := func(text string) bool {
		whole := normalizeSelf(text)
		if matchesSelf(whole, roots, dirs, false) {
			return true
		}
		for _, tok := range selfSplit.Split(text, -1) {
			if tok == "" {
				continue
			}
			if local {
				tok = expandSelfEnv(tok)
			}
			n := cleanSelf(normalizeSelf(tok))
			if matchesSelf(n, roots, dirs, true) {
				return true
			}
			if strings.ContainsAny(tok, "*?[") && globTouches(n, normalizeSelf(cwd), roots, dirs) {
				return true
			}
			if seen[tok] {
				continue
			}
			sep := strings.ContainsAny(tok, "/\\") || strings.HasPrefix(tok, "~")
			if !sep && !strings.Contains(tok, ".") {
				continue
			}
			seen[tok] = true
			if sep {
				if pathResolved >= maxSelfResolve {
					return true // fail closed: too many paths to check them all
				}
				pathResolved++
			} else {
				if bareResolved >= maxSelfBareResolve {
					continue
				}
				bareResolved++
			}
			if r := resolve(tok, cwd); r != "" && matchesSelf(cleanSelf(normalizeSelf(r)), roots, dirs, true) {
				return true
			}
		}
		return false
	}
	check := func(text string) bool {
		if checkOne(text) {
			return true
		}
		if stripped := selfStrip.Replace(text); stripped != text {
			return checkOne(stripped)
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
// a single path-like token. dirs match only exactly, so ~/.claude/projects stays
// usable while moving or replacing ~/.claude itself is refused.
func matchesSelf(n string, roots, dirs []string, asPath bool) bool {
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
		for _, d := range dirs {
			if n == d {
				return true
			}
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

// globTouches reports whether a glob token could name a protected location. The
// pattern is anchored at the session directory when relative and compared segment
// by segment with path.Match; on POSIX a * does not match a leading dot, as in a
// shell. Brace expansion is not understood.
func globTouches(pat, cwd string, roots, dirs []string) bool {
	if !(strings.HasPrefix(pat, "/") || len(pat) > 1 && pat[1] == ':') {
		if cwd == "" {
			return false
		}
		pat = path.Clean(cwd + "/" + pat)
	}
	ps := strings.Split(pat, "/")
	segMatch := func(p, s string) bool {
		if runtime.GOOS != "windows" && strings.HasPrefix(s, ".") && !strings.HasPrefix(p, ".") {
			return false
		}
		ok, err := path.Match(p, s)
		return err == nil && ok
	}
	hit := func(target string, prefix bool) bool {
		ts := strings.Split(target, "/")
		if len(ps) < len(ts) || !prefix && len(ps) != len(ts) {
			return false
		}
		for i := range ts {
			if !segMatch(ps[i], ts[i]) {
				return false
			}
		}
		return true
	}
	for _, r := range roots {
		if hit(r, true) {
			return true
		}
	}
	for _, d := range dirs {
		if hit(d, false) {
			return true
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
		case "opencode_config_dir":
			return agentHome("opencode")
		case "xdg_config_home":
			if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
				return v
			}
			return filepath.Join(home, ".config")
		}
		return m
	})
}
