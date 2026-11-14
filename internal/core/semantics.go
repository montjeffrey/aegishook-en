package core

// These descriptions expose the actual deterministic checks in builtinMatches.
// They are server-owned metadata, not additional rules or model instructions.
type RuleSemantics struct {
	Summary  string            `json:"summary"`
	Checks   []string          `json:"checks"`
	Examples []SemanticExample `json:"examples"`
	Limits   string            `json:"limits"`
}
type SemanticExample struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"argumentsObj"`
}

func shellExample(command string) SemanticExample {
	return SemanticExample{Tool: "bash", Arguments: map[string]any{"command": command}}
}
func builtinSemantics(id string) *RuleSemantics {
	for _, preset := range detailedRuleCatalog {
		if preset.id == id {
			return &RuleSemantics{Summary: preset.summary, Checks: preset.checks, Examples: []SemanticExample{shellExample(preset.example)}, Limits: preset.limits}
		}
	}
	switch id {
	case "R1":
		return &RuleSemantics{
			Summary:  "Detects password-change commands and APIs that change passwords or login state.",
			Checks:   []string{"The tool name contains reset_password, change_password, set_password or logout_user.", "A non-GET / HEAD request with a method whose url path has a /password, /reset-password or /logout segment.", "In shell, detects the passwd, chpasswd and Set-LocalUser commands, with literal command paths and simple sudo wrappers."},
			Examples: []SemanticExample{shellExample("passwd fixture"), {Tool: "change_password", Arguments: map[string]any{"user": "fixture"}}},
			Limits:   "Only the names and argument patterns above are recognized, and it does not judge whether the account is a test account; dynamic wrapper scripts are not guaranteed to be recognized; PowerShell is checked only for parseable literal commands, without interpreting variables or script internals."}
	case "R2":
		return &RuleSemantics{
			Summary:  "Detects writes to protected configuration paths, and scheduled task and firewall changes.",
			Checks:   []string{"The path / file_path / filePath of write / edit (case-insensitive), or shell output redirection, pointing to protected paths such as /etc, /private/etc/, /Library/Launch*, /System/ and the Windows directory on a drive letter; file tools resolve against the project path and handle existing symlinks.", "Also detects explicit commands such as Set-Service, Set-NetFirewallProfile, New/Remove-NetFirewallRule and Register/Unregister/Set-ScheduledTask.", "crontab with arguments other than a lone -l or --list.", "iptables, nft, ufw or firewall-cmd without --list, --status, -l or status in the arguments."},
			Examples: []SemanticExample{shellExample("echo fixture > /etc/aegis-fixture.conf"), shellExample("crontab fixture.txt")},
			Limits:   "Firewall checks use the argument text and cannot prove that every listing operation or unknown subcommand is safe; other configuration management programs are not analyzed automatically."}
	case "R3":
		return &RuleSemantics{
			Summary:  "Detects creating, changing or deleting accounts, user groups, roles and permissions.",
			Checks:   []string{"The tool name contains create_user, delete_user, update_user, create_role, delete_role or grant_permission.", "A non-GET / HEAD request whose url path has a /users, /accounts, /roles or /permissions segment.", "In shell: useradd, userdel, usermod, adduser, deluser, groupadd, groupdel, groupmod, dscl.", "Windows New/Remove/Rename/Enable/Disable-LocalUser, New/Remove-LocalGroup and Add/Remove-LocalGroupMember.", "CREATE / ALTER / DROP USER or ROLE, GRANT or REVOKE in mysql, psql, sqlite or sqlcmd commands."},
			Examples: []SemanticExample{shellExample("userdel fixture"), {Tool: "create_user", Arguments: map[string]any{"name": "fixture"}}},
			Limits:   "Matching is currently by command and API name, so test accounts also match; read-only use of dscl may match too."}
	case "R4":
		return &RuleSemantics{
			Summary:  "Detects deletion of important paths, disk overwrites, and deletion of database objects or whole tables.",
			Checks:   []string{"rm, rmdir or shred with an argument that is the root directory, ~ or $HOME, or contains the path text /etc, /var/lib, /home, /Users or /var/www.", "mkfs, mkfs.ext4, mkfs.xfs, or dd with an argument containing of=/dev/.", "In mysql, psql, sqlite or sqlcmd commands, detects DROP TABLE / DATABASE, TRUNCATE and simple unconditional DELETE FROM.", "When the tool name contains sql, database or query, checks the argument text for DROP TABLE / DATABASE and TRUNCATE."},
			Examples: []SemanticExample{shellExample("rm -rf /var/www/fixture"), shellExample("mysql -e 'DROP TABLE fixture'")},
			Limits:   "Does not distinguish production tables from temporary ones, and backup names also match; does not currently cover all SQL syntax, business DELETE APIs or UPDATE rewrites."}
	case "R5":
		return &RuleSemantics{
			Summary:  "Detects system shutdown, process termination and service stop or restart.",
			Checks:   []string{"Shell commands shutdown, reboot, halt, poweroff, killall or pkill, and Stop/Restart-Service, Stop-Process and Stop/Restart-Computer.", "systemctl, service or launchctl with the word stop, restart, disable, unload or bootout in the arguments."},
			Examples: []SemanticExample{shellExample("systemctl restart fixture"), shellExample("pkill fixture")},
			Limits:   "Does not tell whether a process serves real business; pkill / killall match directly by command, while plain kill and other service managers are not in the current detection list."}
	case "R6":
		return &RuleSemantics{
			Summary:  "Detects the explicit flood argument of hping3.",
			Checks:   []string{"A hping3 command in shell with --flood in the arguments."},
			Examples: []SemanticExample{shellExample("hping3 --flood example.test")},
			Limits:   "Currently covers only the explicit pattern above and does not compute concurrency, rate or duration; other load-testing tools and dynamic loops are handled by the current review mode."}
	case "A7":
		return &RuleSemantics{
			Summary:  "Matches file reads, searches and directory queries by tool name.",
			Checks:   []string{"The tool name is exactly read, grep, find, ls or glob (case-insensitive).", "This rule's decision is used only when no enabled reject rule matches; the authorization scope check still runs first."},
			Examples: []SemanticExample{{Tool: "read", Arguments: map[string]any{"path": "README.md"}}},
			Limits:   "Matches the tool name and does not parse same-named commands in shell; does not prove that a same-named tool from another extension has no write side effects."}
	}
	return nil
}
