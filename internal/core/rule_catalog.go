package core

import (
	"encoding/json"
	"fmt"
)

// Coverage was compared with ARTEX db/db.go at 8dae851b9b622f2ff2631f332fde9719d0b16fba.
// Matchers are implemented here against executable inputs, not copied raw-input regexes.
// The original R1–R6 remain editable independent rules for compatibility.
type detailedRule struct {
	id, name, parent string
	priority         int
	optIn            bool
	summary          string
	checks           []string
	example, limits  string
}

const shellRuleLimits = "Only checks parseable literal commands, simple sudo / env / command wrappers and sh / bash -c; does not expand variables, aliases or external scripts. Printing a command as an example does not match."
const databaseRuleLimits = "Only checks inline statements of known database clients or the query / sql / statement / command arguments of the matching database tools; does not read external script files and does not cover all database syntax."
const scriptRuleLimits = "Only checks explicit calls in python -c, node -e and the code / script arguments of the matching execution tools, skipping comments and string examples; does not resolve aliases, dynamic methods or external files, and does not execute code."

var detailedRuleCatalog = []detailedRule{
	{"R_SYS_RM", "Recursive delete rm -r / rm -rf", "R4", 100, false,
		"Detects rm commands with recursive flags or with root protection disabled.", []string{"rm with -r / -R (including combined short flags), --recursive or --no-preserve-root, and with a deletion target.", "Text after -- is treated as file names; --help / --version do not match."}, "rm -rf ./fixture", shellRuleLimits},
	{"R_SYS_PATH", "Delete critical system directories", "R4", 100, false,
		"Detects rm / rmdir / shred / wipe deleting critical directories and their contents.", []string{"Checks the root directory and /etc, /bin, /usr, /boot, /var, /lib, /lib64, /sys, /proc, /dev, /sbin, /root, /home, /Users, /System, /Library.", "Relative paths are resolved against the project path; a path must equal the directory or be inside it, with no substring matching."}, "rm /etc/fixture.conf", shellRuleLimits},
	{"R_SYS_FORMAT", "Disk format mkfs / Format-Volume", "R4", 100, false,
		"Detects filesystem formatting and macOS / Windows disk wipe commands.", []string{"mkfs, mkfs.*, Format-Volume, Clear-Disk.", "diskutil subcommands eraseDisk, eraseVolume and partitionDisk."}, "mkfs.ext4 /dev/sdb", shellRuleLimits + " PowerShell is supported only for parseable literal commands."},
	{"R_SYS_DD", "Overwrite a disk device with dd / redirection", "R4", 100, false,
		"Detects dd output or shell output redirection aimed at a disk device.", []string{"The of= argument of dd, or a > / >> / &> output target, pointing to sd, vd, xvd, nvme, mmcblk, disk or rdisk devices under /dev.", "/dev/null, regular files and read-only access to a disk are not treated as overwrites."}, "dd if=fixture.img of=/dev/sdb", shellRuleLimits},
	{"R_SYS_FORK", "Fork bomb", "R6", 100, false,
		"Detects a recursive background-pipe function that is declared and then called.", []string{"A shell function whose background pipe calls itself on both sides, and the function is invoked in the command.", "Supports the classic :(){ :|:& };: and named functions with the same structure."}, ":(){ :|:& };:", "Covers only the syntax above; does not estimate the resource use of arbitrary loops or programs."},
	{"R_SYS_SHUTDOWN", "Shutdown / reboot", "R5", 100, false,
		"Detects system shutdown, reboot and switching to a shutdown runlevel.", []string{"shutdown, reboot, halt, poweroff, Stop-Computer, Restart-Computer.", "init / telinit with argument 0 or 6."}, "init 6", shellRuleLimits},
	{"R_SYS_KILL", "Mass process termination", "R5", 100, false,
		"Detects sending a signal to all processes or forced mass termination.", []string{"kill with a target that includes -1; killall / pkill using -9, -KILL or -SIGKILL.", "A plain kill of a single PID does not match this rule; the original R5 still checks process stops."}, "kill -9 -1", shellRuleLimits},
	{"R_SYS_WIPE", "Disk wipe shred / wipe", "R4", 100, false,
		"Detects wipe operations on disk devices.", []string{"shred, wipe or wipefs targeting a disk device under /dev.", "wipefs with -n / --no-act only previews and does not match this rule."}, "shred /dev/sdb", shellRuleLimits},
	{"R_SYS_FIREWALL", "Flush firewall rules", "R2", 100, false,
		"Detects firewall flush and reset.", []string{"iptables / ip6tables -F or --flush (including a named table); nft flush ruleset / table / chain.", "ufw reset; merely listing rules does not match this rule."}, "iptables -t nat -F", shellRuleLimits},
	{"R_DB_DROP", "SQL DROP database / table / schema", "R4", 90, false,
		"Detects DROP statements that delete database objects.", []string{"Inline SQL of mysql / mariadb -e, psql / sqlcmd -c / -Q or sqlite3, or the statement argument of a SQL tool.", "DROP DATABASE / TABLE / SCHEMA / INDEX / VIEW / TABLESPACE / USER / ROLE, ignoring SQL comments and string constants."}, "psql -c 'DROP SCHEMA fixture CASCADE'", databaseRuleLimits},
	{"R_DB_TRUNCATE", "SQL TRUNCATE table", "R4", 90, false,
		"Detects TRUNCATE and TRUNCATE TABLE statements.", []string{"In a database execution context, checks for TRUNCATE at the start of a statement; supports several semicolon-separated statements.", "TRUNCATE examples inside strings and SQL comments are ignored."}, "mysql -e 'TRUNCATE TABLE fixture'", databaseRuleLimits},
	{"R_DB_DELETE", "SQL unconditional DELETE", "R4", 90, false,
		"Detects DELETE FROM statements without a WHERE clause.", []string{"In a database execution context, checks DELETE FROM and matches when the current statement has no WHERE clause.", "Only checks whether WHERE is missing and does not prove the WHERE condition is meaningful; DELETE WHERE 1=1 and similar are handled by other review."}, "mysql -e 'DELETE FROM fixture'", databaseRuleLimits},
	{"R_DB_MONGO", "MongoDB drop / dropDatabase", "R4", 90, false,
		"Detects MongoDB database or collection deletion.", []string{"mongo / mongosh --eval, or the query / script / command of a MongoDB tool.", "dropDatabase, dropCollection and collection drop calls on the db object, skipping comments and string content."}, "mongosh --eval 'db.fixture.drop()'", databaseRuleLimits},
	{"R_DB_REDIS", "Redis FLUSHALL / FLUSHDB", "R4", 90, false,
		"Detects Redis flushing all databases or the current database.", []string{"The actual redis-cli subcommand is FLUSHALL / FLUSHDB (connection options and ASYNC / SYNC are supported).", "The command / cmd argument of Redis tools is also checked; GET FLUSHALL is only a key read and does not match."}, "redis-cli -n 1 FLUSHDB ASYNC", databaseRuleLimits},
	{"R_HTTP_DELETE", "HTTP DELETE · curl / wget / request tools", "R4", 80, false,
		"Detects HTTP DELETE requests and their command-line arguments.", []string{"curl -X DELETE, -XDELETE, --request DELETE / --request=DELETE; wget --method DELETE / --method=DELETE.", "A request tool with both url and method=DELETE; PowerShell Invoke-WebRequest / Invoke-RestMethod -Method Delete."}, "curl -X DELETE https://example.test/fixture", shellRuleLimits},
	{"R_HTTP_PYTHON", "Python HTTP client DELETE", "R4", 80, false,
		"Detects HTTP delete calls in inline Python code.", []string{".delete(...) calls on requests / httpx / aiohttp / urllib.request / session / client.", "session / client are recognized by naming convention; the actual type of a dynamic object cannot be confirmed."}, "python -c 'import requests; requests.delete(\"https://example.test/fixture\")'", scriptRuleLimits},
	{"R_HTTP_SCRIPT", "JavaScript / generic HTTP DELETE method", "R4", 80, false,
		"Detects axios.delete and DELETE method declarations in request calls.", []string{"axios.delete(...) in executed code.", "method: 'DELETE' or method='DELETE' in code that contains fetch / axios / requests / httpx request calls."}, "node -e 'fetch(\"https://example.test/fixture\", {method: \"DELETE\"})'", scriptRuleLimits + " Request calls and method declarations are associated by snippet; no full data-flow analysis is done."},
	{"R_HTTP_CLEAR", "Bulk clear / destroy endpoints", "R4", 80, false,
		"Detects write requests sent to clear or destroy paths.", []string{"POST / PUT / PATCH / DELETE requests whose URL path segment contains clear, wipe, flush, purge, truncate, drop, destroy, factory-reset, factory_reset, reset-all, reset_all.", "Checks structured requests and curl / wget / PowerShell requests; GET / HEAD and text in URL query parameters do not match."}, "curl -X POST https://example.test/api/clear", shellRuleLimits + " URLs assembled inside scripts are not checked."},
	{"R_NET_PIPE", "Data exfiltration pipe (optional)", "", 80, true,
		"Detects a download / network command piped into another network client.", []string{"The actual commands on both sides of the pipe are curl, wget, nc, ncat or netcat.", "This structure is also used for normal data transfer, so it is disabled by default; enable it only when you need to restrict it."}, "curl https://example.test/data | nc example.test 9000", "A structural match does not prove data was leaked; external scripts, dynamic commands and target ownership are not checked."},
}

// Add only missing catalog entries, atomically with one configuration version.
// Existing records are never replaced, including edited builtin matchers.
func (e *Engine) upgradeBuiltinRules(saved []Rule) error {
	byID := map[string]Rule{}
	for _, r := range saved {
		byID[r.ID] = r
	}
	var added []Rule
	for _, r := range BuiltinRules() {
		if _, exists := byID[r.ID]; exists {
			continue
		}
		for _, p := range detailedRuleCatalog {
			if p.id != r.ID || p.parent == "" {
				continue
			}
			if parent, ok := byID[p.parent]; ok {
				// A disabled/replaced/allowing/restricted parent is an intentional
				// policy choice; do not silently re-enable that category on upgrade.
				if !parent.Enabled || parent.Decision != "reject" || parent.Matcher != "semantic" || parent.Tool != "" {
					r.Enabled = false
				}
			}
		}
		added = append(added, r)
	}
	if len(added) == 0 {
		return nil
	}
	tx, err := e.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	put := func(kind, id string, value any) error {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO records(kind,id,payload) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET payload=excluded.payload", kind, id, b)
		return err
	}
	for _, r := range added {
		if err = put("rules", r.ID, r); err != nil {
			return err
		}
	}
	next := e.Settings
	next.Version++
	if err = put("settings", "current", next); err != nil {
		return err
	}
	if err = put("settings_versions", fmt.Sprint(next.Version), next); err != nil {
		return err
	}
	audit := Audit{ID: ID(), Action: "rule.catalog_upgrade", Subject: "builtin", Detail: fmt.Sprintf("added %d built-in rules; existing rules kept, and new entries stay disabled when the related category was disabled or customized", len(added)), CreatedAt: e.clock()}
	if err = put("audit", audit.ID, audit); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	e.Settings = next
	return nil
}
