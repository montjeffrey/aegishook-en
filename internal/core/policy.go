package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"mvdan.cc/sh/v3/syntax"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var secretKey = regexp.MustCompile(`(?i)(password|passwd|secret|token|api.?key|authorization|cookie|credential)`)
var textSecrets = regexp.MustCompile(`(?i)((?:password|passwd|token|api[_-]?key|secret|authorization)\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&]+)`)
var flagSecrets = regexp.MustCompile(`(?i)(--?(?:password|passwd|token|api[_-]?key|secret)\s+)(?:"[^"]*"|'[^']*'|[^\s,;&]+)`)
var jsonSecrets = regexp.MustCompile(`(?i)("(?:password|passwd|secret|token|api.?key|authorization|cookie|credential)"\s*:\s*)"(?:[^"\\]|\\.)*"`)
var urlSecrets = regexp.MustCompile(`(?i)(https?://)[^\s/@:]+:[^\s/@]+@`)
var bearer = regexp.MustCompile(`(?i)\bBearer\s+[a-z0-9._~+/-]+`)

// A bare -p is a port, a path or a flag in most tools, so it is not in flagSecrets.
// It is a password for sshpass (-p <password>) and for the MySQL family, where the
// value must follow -p directly (-pSECRET; a separated word is the database name).
var sshpassFlag = regexp.MustCompile(`(?i)(\bsshpass\b[^\n;|&]*?\s-p\s*)(?:"[^"]*"|'[^']*'|[^\s,;&]+)`)
var mysqlFlag = regexp.MustCompile(`(?i)(\b(?:mysql|mysqldump|mysqladmin|mariadb|mariadb-dump)(?:\.exe)?\b[^\n;|&]*?\s-p)(?:"[^"]*"|'[^']*'|[^\s,;&]+)`)

const redactedMark = "[REDACTED]"

// shellSyntax are the characters that make a "secret" something a shell would run
// or redirect rather than a literal value.
const shellSyntax = "$`();|&<>\n\r"

// redactStats counts what redaction hid. shell counts hidden spans that contain
// shell syntax, which would hide executable code from the reviewer.
type redactStats struct{ hidden, shell int }

// replaceCounting works like ReplaceAllString, but also counts the matches whose
// replacement differs from the matched text, i.e. spans that were really hidden.
func replaceCounting(re *regexp.Regexp, s, template string, st *redactStats) string {
	idx := re.FindAllStringSubmatchIndex(s, -1)
	if len(idx) == 0 {
		return s
	}
	var out []byte
	last := 0
	for _, m := range idx {
		out = append(out, s[last:m[0]]...)
		rep := re.ExpandString(nil, template, s, m)
		if string(rep) != s[m[0]:m[1]] {
			st.hidden++
			from := m[0]
			if len(m) >= 4 && m[3] >= 0 {
				from = m[3] // the kept prefix is not hidden; the rest is
			}
			if strings.ContainsAny(s[from:m[1]], shellSyntax) {
				st.shell++
			}
		}
		out = append(out, rep...)
		last = m[1]
	}
	return string(append(out, s[last:]...))
}

// redactSecretsStats masks secret-shaped text and counts the hidden spans.
func redactSecretsStats(s string, st *redactStats) string {
	s = replaceCounting(bearer, s, "Bearer [REDACTED]", st)
	s = replaceCounting(jsonSecrets, s, `${1}"[REDACTED]"`, st)
	s = replaceCounting(flagSecrets, s, "${1}[REDACTED]", st)
	s = replaceCounting(sshpassFlag, s, "${1}[REDACTED]", st)
	s = replaceCounting(mysqlFlag, s, "${1}[REDACTED]", st)
	s = replaceCounting(urlSecrets, s, "${1}[REDACTED]@", st)
	s = replaceCounting(textSecrets, s, "${1}[REDACTED]", st)
	s = replaceCounting(bearer, s, "Bearer [REDACTED]", st)
	return s
}

// redactSecrets masks secret-shaped text and reports how many spans were hidden.
func redactSecrets(s string) (string, int) {
	var st redactStats
	s = redactSecretsStats(s, &st)
	return s, st.hidden
}

// RedactText is for logs, errors, results and other display-only text; it also
// truncates. Never use it on content an approval is bound to (see redactArguments).
func RedactText(s string) string {
	s, _ = redactSecrets(s)
	if len(s) > 24000 {
		s = s[:24000] + "…[truncated]"
	}
	return s
}
func Redact(v any) any {
	out, _ := redactArguments(v)
	return out
}

// commandText returns the command a tool call will execute, using the same
// classification as the built-in rules: the Bash tool, or any tool whose arguments
// carry a command or cmd string. ok is false for every other tool.
func commandText(in ReviewInput) (cmd string, ok bool) {
	cmd = argumentText(in.Arguments, "command", "cmd")
	return cmd, strings.EqualFold(in.ToolName, "bash") || cmd != ""
}

// commandShellSpans counts the secret-shaped spans inside the executed command that
// contain shell syntax. It is zero for tools that do not execute a command.
func commandShellSpans(in ReviewInput) int {
	cmd, ok := commandText(in)
	if !ok {
		return 0
	}
	var st redactStats
	redactSecretsStats(cmd, &st)
	return st.shell
}

// redactArguments masks secrets in tool-call arguments without truncating them,
// and returns how many spans were hidden. A reviewer must always be shown the
// whole call, and must be told when part of it was masked.
func redactArguments(v any) (any, int) {
	var st redactStats
	out := redactArgumentsStats(v, &st)
	return out, st.hidden
}

// redactArgumentsStats is redactArguments with the shell-syntax count. A value under
// a secret-named key is data for the tool, not text a shell parses, so it is
// counted as hidden but never as shell syntax.
func redactArgumentsStats(v any, st *redactStats) any {
	switch x := v.(type) {
	case map[string]any:
		o := map[string]any{}
		for k, v := range x {
			if secretKey.MatchString(k) {
				o[k] = redactedMark
				if v != redactedMark {
					st.hidden++
				}
			} else {
				o[k] = redactArgumentsStats(v, st)
			}
		}
		return o
	case []any:
		o := make([]any, len(x))
		for i, v := range x {
			o[i] = redactArgumentsStats(v, st)
		}
		return o
	case string:
		return redactSecretsStats(x, st)
	default:
		return v
	}
}
func BuiltinRules() []Rule {
	names := []string{"Block password and login state changes", "Block target system configuration changes", "Block account and permission changes", "Block destruction of critical data and files", "Block stopping or restarting business services", "Block explicit denial-of-service operations"}
	out := []Rule{}
	for i, n := range names {
		out = append(out, Rule{ID: fmt.Sprintf("R%d", i+1), Name: n, Decision: "reject", Enabled: true, Builtin: true, Matcher: "semantic", Target: "arguments"})
	}
	out = append(out, Rule{ID: "A7", Name: "Read and query tools", Decision: "approve", Enabled: true, Builtin: true, Matcher: "semantic", Target: "arguments"})
	for _, preset := range detailedRuleCatalog {
		out = append(out, Rule{ID: preset.id, Name: preset.name, Decision: "reject", Enabled: !preset.optIn, Builtin: true, Matcher: "semantic", Target: "arguments", Priority: preset.priority})
	}
	for i := range out {
		out[i] = normalizeRule(out[i])
	}
	return out
}
func reject(id, what string) *Decision {
	return &Decision{Decision: "reject", RuleID: id, Comment: "Actual operation: " + what + "; Consequence if successful: may change protected system state; Matched rule: " + id + ". Use a read-only verification method instead."}
}
func valueAt(m map[string]any, path string) any {
	var v any = m
	for _, p := range strings.Split(path, ".") {
		o, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = o[p]
	}
	return v
}
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func has(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
func builtinMatches(input ReviewInput, cwd string) map[string]string {
	return builtinMatchesPaths(input, cwd, policyPath)
}
func builtinMatchesPaths(input ReviewInput, cwd string, resolve func(string, string) string) map[string]string {
	hits := map[string]string{}
	add := func(id, what string) { hits[id] = what }
	name := strings.ToLower(input.ToolName)
	b, _ := json.Marshal(input.Arguments)
	raw := strings.ToLower(string(b))
	cmd := argumentText(input.Arguments, "command", "cmd")
	// Only confident effects trigger hard rules. Opaque programs fall through to the configured reviewer.
	if has(name, "reset_password", "change_password", "set_password", "logout_user") {
		add("R1", "change a password or user login state")
	}
	if has(name, "create_user", "delete_user", "update_user", "create_role", "delete_role", "grant_permission") {
		add("R3", "change accounts or permissions")
	}
	method := strings.ToUpper(fmt.Sprint(input.Arguments["method"]))
	requestURL := fmt.Sprint(input.Arguments["url"])
	if method != "GET" && method != "HEAD" && method != "<nil>" {
		if regexp.MustCompile(`(?i)/(password|reset-password|logout)(/|\?|$)`).MatchString(requestURL) {
			add("R1", "change a password or login state through an API")
		}
		if regexp.MustCompile(`(?i)/(users|accounts|roles|permissions)(/|\?|$)`).MatchString(requestURL) {
			add("R3", "change accounts or permissions through an API")
		}
	}
	if name == "write" || name == "edit" {
		p := argumentText(input.Arguments, "path", "file_path", "filePath")
		if protectedPath(resolve(p, cwd)) {
			add("R2", "write a system configuration file")
		}
	}
	if name == "bash" || cmd != "" {
		tree, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
		if err == nil {
			syntax.Walk(tree, func(n syntax.Node) bool {
				if red, ok := n.(*syntax.Redirect); ok && red.Word != nil {
					if red.Op == syntax.RdrOut || red.Op == syntax.AppOut || red.Op == syntax.RdrAll || red.Op == syntax.AppAll {
						if protectedPath(resolve(literalWord(red.Word), cwd)) {
							add("R2", "redirect output into system configuration")
						}
					}
				}
				c, ok := n.(*syntax.CallExpr)
				if !ok || len(c.Args) == 0 {
					return true
				}
				words := []string{}
				for _, w := range c.Args {
					words = append(words, literalWord(w))
				}
				exe := strings.ToLower(filepath.Base(words[0]))
				args := strings.ToLower(strings.Join(words[1:], " "))
				if exe == "sudo" && len(words) > 1 {
					exe = strings.ToLower(filepath.Base(words[1]))
					args = strings.ToLower(strings.Join(words[2:], " "))
				}
				switch exe {
				case "passwd", "chpasswd", "set-localuser":
					add("R1", "change a user password")
				case "useradd", "userdel", "usermod", "adduser", "deluser", "groupadd", "groupdel", "groupmod", "dscl", "new-localuser", "remove-localuser", "rename-localuser", "enable-localuser", "disable-localuser", "new-localgroup", "remove-localgroup", "add-localgroupmember", "remove-localgroupmember":
					add("R3", "change system accounts or user groups")
				case "shutdown", "reboot", "halt", "poweroff", "killall", "pkill", "stop-service", "restart-service", "stop-process", "stop-computer", "restart-computer":
					add("R5", "stop a system or business process")
				case "systemctl", "service", "launchctl":
					if regexp.MustCompile(`\b(stop|restart|disable|unload|bootout)\b`).MatchString(args) {
						add("R5", "stop or restart a business service")
					}
				case "set-service", "set-netfirewallprofile", "new-netfirewallrule", "remove-netfirewallrule", "register-scheduledtask", "unregister-scheduledtask", "set-scheduledtask":
					add("R2", "change Windows service, firewall or scheduled task configuration")
				case "crontab":
					if !regexp.MustCompile(`^(-l|--list)$`).MatchString(strings.TrimSpace(args)) {
						add("R2", "change a scheduled task")
					}
				case "iptables", "nft", "ufw", "firewall-cmd":
					if !has(args, "--list", "--status", "-l", "status") {
						add("R2", "change firewall configuration")
					}
				case "rm", "rmdir", "shred":
					for _, w := range words[1:] {
						if criticalRemoveTarget(w) {
							add("R4", "delete important files or a data directory")
						}
					}
				case "mkfs", "mkfs.ext4", "mkfs.xfs":
					add("R4", "format a filesystem")
				case "dd":
					if has(args, "of=/dev/") {
						add("R4", "overwrite a disk device")
					}
				case "hping3":
					if strings.Contains(args, "--flood") {
						add("R6", "send flood traffic")
					}
				}
				if has(exe, "mysql", "psql", "sqlite", "sqlcmd") {
					if regexp.MustCompile(`(?i)\b(drop\s+(table|database)|truncate\s+(table\s+)?\w+)`).MatchString(cmd) {
						add("R4", "delete database objects")
					}
					if regexp.MustCompile(`(?i)\bdelete\s+from\s+\w+\s*(;|"|'|$)`).MatchString(cmd) {
						add("R4", "delete all rows in a table")
					}
					if regexp.MustCompile(`(?i)\b(create|alter|drop)\s+(user|role)\b|\b(grant|revoke)\b`).MatchString(cmd) {
						add("R3", "change database account permissions")
					}
				}
				return true
			})
		}
	}
	if has(name, "sql", "database", "query") {
		if regexp.MustCompile(`(?i)\b(drop\s+(table|database)|truncate\s+)`).MatchString(raw) {
			add("R4", "delete database objects")
		}
	}
	if hasExact(name, "read", "grep", "find", "ls", "glob") {
		add("A7", "read or query")
	}
	return hits
}

// slashClean normalises a path from any OS to forward slashes and cleans it
// lexically, without depending on the host's path separator.
func slashClean(p string) string {
	return path.Clean(strings.ReplaceAll(p, "\\", "/"))
}

// criticalRemoveTargets are directories whose removal (or removal of anything
// under them, on a POSIX absolute path) is refused by R4.
var criticalRemoveTargets = []string{"/etc", "/var/lib", "/home", "/Users", "/var/www"}

// criticalRemoveTarget matches whole path components: /home and /home/u/x match,
// /homework and build/home/x do not. A Windows drive path is refused only when it
// is the user-data or system directory itself or a drive root, so deleting a file in
// a project under a Users directory is not blocked here.
func criticalRemoveTarget(w string) bool {
	if w == "~" || w == "$HOME" {
		return true
	}
	p := slashClean(w)
	if p == "/" {
		return true
	}
	if len(p) >= 2 && p[1] == ':' {
		rest := strings.ToLower(p[2:])
		if rest == "" || rest == "/" || rest == "/users" || rest == "/windows" || rest == "/program files" {
			return true
		}
		return false
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	for _, d := range criticalRemoveTargets {
		if p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

func protectedPath(p string) bool {
	// The server may review calls made on another OS, so neither the host's
	// separator nor filepath semantics may decide what is a system path.
	p = slashClean(p)
	win := strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	if len(win) > 2 && win[1] == ':' {
		win = win[2:]
		if win == "/windows" || strings.HasPrefix(win, "/windows/") {
			return true
		}
	}
	return p == "/etc" || strings.HasPrefix(p, "/etc/") || strings.HasPrefix(p, "/private/etc/") || strings.HasPrefix(p, "/Library/Launch") || strings.HasPrefix(p, "/System/")
}
func Evaluate(in ReviewInput, rules []Rule) *Decision { return evaluateAt(in, rules, "") }

// Legacy records have no matcher/target fields; preserve their original semantics.
func normalizeRule(r Rule) Rule {
	if r.Matcher == "" {
		if r.Builtin {
			r.Matcher = "semantic"
		} else {
			r.Matcher = "regex"
		}
	}
	if r.Target == "" {
		r.Target = "arguments"
	}
	r.Semantics = nil
	if r.Builtin && r.Matcher == "semantic" {
		r.Semantics = builtinSemantics(r.ID)
	}
	return r
}
func sortRules(rules []Rule) {
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Decision != rules[j].Decision {
			return rules[i].Decision == "reject"
		}
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].ID < rules[j].ID
	})
}
func evaluateAt(in ReviewInput, rules []Rule, cwd string) *Decision {
	return evaluatePaths(in, rules, cwd, policyPath)
}
func evaluatePaths(in ReviewInput, rules []Rule, cwd string, resolve func(string, string) string) *Decision {
	// Collect every semantic hit: disabling one rule must not hide another operation.
	hits := builtinMatchesPaths(in, cwd, resolve)
	for id, what := range detailedBuiltinMatches(in, cwd, resolve) {
		hits[id] = what
	}
	ordered := append([]Rule(nil), rules...)
	sortRules(ordered)
	for _, saved := range ordered {
		r := normalizeRule(saved)
		if !r.Enabled || (r.Tool != "" && r.Tool != in.ToolName) {
			continue
		}
		matched, what := false, in.ToolName
		if r.Matcher == "semantic" {
			what, matched = hits[r.ID]
		} else {
			v := any(in.Arguments)
			if r.Target == "toolName" {
				v = in.ToolName
			} else if r.Field != "" {
				v = valueAt(in.Arguments, r.Field)
				if v == nil {
					continue
				}
			}
			if r.Matcher == "contains" {
				matched = strings.Contains(stringValue(v), r.Pattern)
			} else if r.Matcher == "regex" {
				re, err := regexp.Compile(r.Pattern)
				matched = err == nil && re.MatchString(stringValue(v))
			}
		}
		if !matched {
			continue
		}
		consequence := "decide this call by the configured rule"
		if r.Matcher == "semantic" {
			consequence = "may change target system state"
			if r.ID == "A7" {
				consequence = "returns query results"
			}
		}
		comment := "Actual operation: " + what + "; Consequence if successful: " + consequence + "; Matched rule: " + r.ID + " (" + r.Name + ")"
		if r.Message != "" {
			comment += "; " + r.Message
		}
		return &Decision{Decision: r.Decision, RuleID: r.ID, Comment: comment}
	}
	return nil
}
func hasExact(s string, ss ...string) bool {
	for _, x := range ss {
		if s == x {
			return true
		}
	}
	return false
}
func (e *Engine) SaveRule(r Rule) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Builtin is provenance, not an immutable decision or a client-controlled flag.
	known := false
	for _, preset := range BuiltinRules() {
		if preset.ID == r.ID {
			known = true
			break
		}
	}
	if !known && !regexp.MustCompile(`^U_[A-Za-z0-9_-]{1,64}$`).MatchString(r.ID) {
		return errors.New("invalid rule ID; new rule IDs must start with U_")
	}
	if !known && r.Builtin {
		return errors.New("a custom rule cannot be marked as a built-in rule")
	}
	r.Builtin = known
	r = normalizeRule(r)
	if r.Decision != "approve" && r.Decision != "reject" {
		return errors.New("invalid decision")
	}
	if len(r.Pattern) > 2048 || strings.TrimSpace(r.Name) == "" || len(r.Name) > 200 || len(r.Message) > 4000 {
		return errors.New("rule name, expression or message is invalid")
	}
	if !hasExact(r.Target, "arguments", "toolName") {
		return errors.New("invalid match target")
	}
	switch r.Matcher {
	case "semantic":
		if !known {
			return errors.New("semantic matching applies only to built-in rules")
		}
		if r.Target != "arguments" || r.Field != "" || r.Pattern != "" {
			return errors.New("semantic matching does not use an argument path or expression; switch the match method")
		}
	case "regex":
		if r.Pattern == "" {
			return errors.New("enter a match expression")
		}
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return errors.New("regular expression is invalid")
		}
	case "contains":
		if r.Pattern == "" {
			return errors.New("enter match text")
		}
	default:
		return errors.New("invalid match method")
	}
	return e.change("rules", r.ID, r, "rule.save", r.Name)
}
func (e *Engine) DeleteRule(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !strings.HasPrefix(id, "U_") {
		return errors.New("for a built-in rule, disable it, or edit it and then restore the default")
	}
	return e.change("rules", id, nil, "rule.delete", id)
}
func (e *Engine) SaveScope(s Scope) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s.ID == "" {
		s.ID = ID()
	}
	if s.NodeID != "" {
		if !hasExact(s.Platform, "linux", "darwin", "windows") || !agentPathAbs(s.Project, s.Platform) {
			return errors.New("a remote authorization scope needs a node platform and an absolute project path")
		}
	} else if !filepath.IsAbs(s.Project) {
		return errors.New("enter an absolute project path")
	}
	if s.Name == "" {
		return errors.New("enter a name and an absolute project path")
	}
	if s.NodeID == "" {
		p, err := filepath.EvalSymlinks(s.Project)
		if err != nil {
			return errors.New("project path does not exist")
		}
		s.Project = p
	}
	for _, p := range s.Paths {
		if (s.NodeID == "" && !filepath.IsAbs(p)) || (s.NodeID != "" && !agentPathAbs(p, s.Platform)) {
			return errors.New("allowed paths must be absolute")
		}
	}
	for _, t := range s.Targets {
		if t == "" || strings.ContainsAny(t, "/ :") {
			return errors.New("target must be an exact host name or IP, without protocol or port")
		}
	}
	return e.change("scopes", s.ID, s, "scope.save", s.Name)
}
func (e *Engine) DeleteScope(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.change("scopes", id, nil, "scope.delete", id)
}
func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// withinResolved is within for a local call: the project root as configured and the
// cwd as the client sent it may be spelled differently (an 8.3 short name and its
// long name, a symlinked root, another case). When the textual comparison fails,
// both sides are compared again after resolving them on the filesystem, so a scope
// is never skipped just because of spelling. Remote nodes use agentWithin, which
// compares normalised strings and never touches this machine's filesystem.
func withinResolved(root, p string) bool {
	return within(root, p) || within(policyPath(root, ""), policyPath(p, ""))
}

func checkScope(in ReviewInput, cwd string, scopes []Scope) *Decision {
	return checkScopePaths(in, cwd, scopes, policyPath, withinResolved)
}
func checkScopePaths(in ReviewInput, cwd string, scopes []Scope, resolve func(string, string) string, contains func(string, string) bool) *Decision {
	for _, s := range scopes {
		if !contains(s.Project, cwd) {
			continue
		}
		if hasExact(strings.ToLower(in.ToolName), "read", "write", "edit", "ls", "find", "grep", "glob") && len(s.Paths) > 0 {
			if p := argumentText(in.Arguments, "path", "file_path", "filePath"); p != "" {
				p = resolve(p, cwd)
				okPath := false
				for _, root := range s.Paths {
					if contains(resolve(root, cwd), p) {
						okPath = true
					}
				}
				if !okPath {
					return reject("SCOPE_PATH", "access a path outside the configured authorization scope")
				}
			}
		}
		if len(s.Targets) > 0 {
			for _, field := range []string{"url", "host", "target"} {
				if v, ok := in.Arguments[field].(string); ok {
					host := v
					if u, err := url.Parse(v); err == nil && u.Host != "" {
						host = u.Hostname()
					}
					if !hasExact(strings.ToLower(host), s.Targets...) {
						return reject("SCOPE_TARGET", "access a target outside the configured authorization scope")
					}
				}
			}
		}
	}
	return nil
}

// Resolve existing parent symlinks too, so a new file under a symlink is checked against its real destination.
func policyPath(p, cwd string) string { return resolvePolicyPath(p, cwd, 0) }

// rootedPath reports a path that starts at a filesystem root without a drive, such
// as /etc/hosts. filepath.IsAbs is false for it on Windows, but it must never be
// joined to the working directory.
func rootedPath(p string) bool { return strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) }

func resolvePolicyPath(p, cwd string, depth int) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) && !rootedPath(p) {
		if cwd == "" {
			return filepath.Clean(p)
		}
		p = filepath.Join(cwd, p)
	}
	suffix := []string{}
	cur := filepath.Clean(p)
	for {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			parts := []string{real}
			for i := len(suffix) - 1; i >= 0; i-- {
				parts = append(parts, suffix[i])
			}
			return filepath.Join(parts...)
		}
		// A dangling symlink still writes through to its target, so follow it by hand.
		if target, err := os.Readlink(cur); err == nil && depth < 16 {
			if !filepath.IsAbs(target) && !rootedPath(target) {
				target = filepath.Join(filepath.Dir(cur), target)
			}
			parts := []string{target}
			for i := len(suffix) - 1; i >= 0; i-- {
				parts = append(parts, suffix[i])
			}
			return resolvePolicyPath(filepath.Join(parts...), "", depth+1)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(p)
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}
func literalWord(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var out strings.Builder
	var part func(syntax.WordPart) bool
	part = func(p syntax.WordPart) bool {
		switch x := p.(type) {
		case *syntax.Lit:
			out.WriteString(x.Value)
			return true
		case *syntax.SglQuoted:
			out.WriteString(x.Value)
			return true
		case *syntax.DblQuoted:
			for _, p := range x.Parts {
				if !part(p) {
					return false
				}
			}
			return true
		default:
			return false
		}
	}
	for _, p := range w.Parts {
		if !part(p) {
			return ""
		}
	}
	return out.String()
}

func argumentText(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := args[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
