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
