package core

import (
	"os"
	"path/filepath"
	"testing"
)

// POSIX-style protected paths must be caught on every OS, including Windows where
// filepath.Clean rewrites separators and IsAbs is false for a drive-less root.
func TestProtectedPathAcrossSeparators(t *testing.T) {
	for p, want := range map[string]bool{
		"/etc/hosts":                 true,
		"/etc":                       true,
		"/etc/../etc/passwd":         true,
		"/private/etc/hosts":         true,
		"/System/Library/x":          true,
		"/Library/LaunchDaemons/x":   true,
		`C:\Windows\System32\x`:      true,
		"c:/windows/system32/x":      true,
		"/etcetera/x":                false,
		"/home/user/etc/hosts":       false,
		`C:\Users\someone\notes.txt`: false,
		"/var/www/html/index.html":   false,
		"relative/etc/hosts":         false,
		`C:\WindowsApps\x`:           false,
	} {
		if got := protectedPath(p); got != want {
			t.Errorf("protectedPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestRootedPathIsNotJoinedToCwd(t *testing.T) {
	cwd := t.TempDir()
	got := filepath.ToSlash(policyPath("/etc/fixture.conf", cwd))
	if !protectedPath(got) {
		t.Fatalf("rooted path was resolved under the working directory: %q", got)
	}
}

func TestDanglingSymlinkIsFollowedToItsTarget(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink("/etc/aegis-missing-fixture.conf", link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if got := filepath.ToSlash(policyPath(link, "")); !protectedPath(got) {
		t.Fatalf("dangling symlink to a protected path was not followed: %q", got)
	}
}

// R4 must match whole path components. Ordinary deletions in a project that merely
// has a "home" or "Users" component are not data-directory deletions.
func TestRemoveTargetMatchesWholeComponents(t *testing.T) {
	for p, want := range map[string]bool{
		"/":                           true,
		"/etc":                        true,
		"/etc/hosts":                  true,
		"/home":                       true,
		"/home/user/x":                true,
		"/Users/someone/x":            true,
		"/var/www/html":               true,
		"/var/lib/mysql":              true,
		"~":                           true,
		"$HOME":                       true,
		`C:\Users`:                    true,
		`C:\Windows`:                  true,
		`C:\`:                         true,
		`C:\Users\alice\proj\out.txt`: false,
		`build\homework\x.o`:          false,
		"build/homework/x.o":          false,
		"build/home/x.o":              false,
		"/homework/x":                 false,
		"/etcetera":                   false,
		"/var/library":                false,
		"out.txt":                     false,
		"./target":                    false,
	} {
		if got := criticalRemoveTarget(p); got != want {
			t.Errorf("criticalRemoveTarget(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestRmOfProjectFilesIsNotRejectedByR4(t *testing.T) {
	e := engine(t)
	for _, cmd := range []string{`rm C:\Users\alice\proj\out.txt`, `rm build\homework\x.o`, "rm build/homework/x.o"} {
		r, err := e.Submit(input(ID(), "Bash", map[string]any{"command": cmd}))
		if err != nil {
			t.Fatal(err)
		}
		if r.RuleID == "R4" {
			t.Errorf("%q wrongly matched R4", cmd)
		}
	}
	r, _ := e.Submit(input(ID(), "Bash", map[string]any{"command": "rm -rf /home/user"}))
	if r.Decision != "reject" {
		t.Errorf("rm -rf /home/user must still be rejected, got %s / %s", r.Decision, r.RuleID)
	}
}

// The server can review calls made by a client on another OS, so what counts as a
// system path must not depend on the host's separator. These cases are written with
// backslashes on purpose: on a POSIX host filepath treats them as ordinary
// characters, which is what made the first version host-dependent.
func TestSystemPathsAreRecognisedOnAnyHostOS(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\Windows`:                  true,
		`c:\windows`:                  true,
		`C:\Windows\`:                 true,
		`C:/Windows`:                  true,
		`C:\Users`:                    true,
		`C:\Users\`:                   true,
		`C:\`:                         true,
		`C:`:                          true,
		`C:\Program Files`:            true,
		`C:\Users\alice\proj\out.txt`: false,
		`C:\Windows\Temp\x`:           false, // only the directory itself is critical for remove
		`build\homework\x.o`:          false,
		`\home\user`:                  true,
		`\etc\hosts`:                  true,
		`/home/../home/user`:          true,
		`..\home\user`:                false,
	} {
		if got := criticalRemoveTarget(p); got != want {
			t.Errorf("criticalRemoveTarget(%q) = %v, want %v", p, got, want)
		}
	}
	for p, want := range map[string]bool{
		`\etc\hosts`:                            true,
		`\etc`:                                  true,
		`C:\Windows\System32\drivers\etc\hosts`: true,
		`c:\windows\system32`:                   true,
		`C:\Users\alice\notes.txt`:              false,
		`\home\alice\etc\hosts`:                 false,
		`/etc\hosts`:                            true,
		`\Library\LaunchDaemons\x`:              true,
	} {
		if got := protectedPath(p); got != want {
			t.Errorf("protectedPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestQuotedWindowsPathsKeepTheirSeparatorsForR4(t *testing.T) {
	e := engine(t)
	for _, cmd := range []string{`rm 'C:\Users\alice\proj\out.txt'`, `rm 'build\homework\x.o'`} {
		r, err := e.Submit(input(ID(), "Bash", map[string]any{"command": cmd}))
		if err != nil {
			t.Fatal(err)
		}
		if r.RuleID == "R4" {
			t.Errorf("%q wrongly matched R4", cmd)
		}
	}
	r, err := e.Submit(input(ID(), "Bash", map[string]any{"command": `rm -r 'C:\Users'`}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Decision != "reject" {
		t.Errorf(`rm -r 'C:\Users': got %s / %s, want a rejection`, r.Decision, r.RuleID)
	}
}
