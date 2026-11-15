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
