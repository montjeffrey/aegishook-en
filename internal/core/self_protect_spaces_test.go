package core

import (
	"os"
	"path/filepath"
	"testing"
)

// touches runs the fixed self-protection check on one call, as a local one.
func touches(t *testing.T, cwd string, set selfSet, tool string, args map[string]any) bool {
	t.Helper()
	return selfTouches(ReviewInput{ToolName: tool, Arguments: args}, cwd, set, policyPath, true)
}

// callsNaming returns the same path as a path-typed argument and quoted inside two
// shell commands, which is how a path with spaces reaches the check.
func callsNaming(p string) map[string]map[string]any {
	return map[string]map[string]any{
		"file_path":        {"file_path": p},
		"double-quoted":    {"command": `type "` + p + `"`},
		"single-quoted":    {"command": "cat '" + p + "'"},
		"quoted after cmd": {"command": `cd /tmp && cat "` + p + `" | head`},
	}
}

// A path with a space in it is not split into tokens by the shell when quoted, and
// a path-typed argument is one path. Both must be resolved as a whole, or a symlink
// in a directory with a space in its name hides a protected file.
func TestSelfProtectResolvesPathsWithSpacesAsAWhole(t *testing.T) {
	isolatedHome(t)
	base := t.TempDir()
	realDir := filepath.Join(base, "Real Data Folder", "gate data")
	if err := os.MkdirAll(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "admin.token"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "My Link Folder")
	if err := os.Symlink(filepath.Join(base, "Real Data Folder"), link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	cwd := filepath.Join(base, "work tree")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	set := selfSet{roots: []string{realDir}, dirs: []string{realDir}}
	for name, args := range callsNaming(filepath.Join(link, "gate data", "admin.token")) {
		t.Run("file via link "+name, func(t *testing.T) {
			if !touches(t, cwd, set, "Bash", args) {
				t.Fatalf("not rejected: %v", args)
			}
		})
	}
	for name, args := range callsNaming(filepath.Join(link, "gate data")) {
		t.Run("directory via link "+name, func(t *testing.T) {
			if !touches(t, cwd, selfSet{dirs: []string{realDir}}, "Bash", args) {
				t.Fatalf("a protected directory was reached through a link: %v", args)
			}
		})
	}
	// A path with spaces that is not protected stays allowed.
	for name, args := range callsNaming(filepath.Join(link, "other data", "notes.txt")) {
		t.Run("unrelated "+name, func(t *testing.T) {
			if touches(t, cwd, set, "Bash", args) {
				t.Fatalf("an unrelated path was rejected: %v", args)
			}
		})
	}
}

// A glob over a protected directory is matched even when the path holds spaces.
func TestSelfProtectMatchesGlobsInPathsWithSpaces(t *testing.T) {
	isolatedHome(t)
	base := t.TempDir()
	root := filepath.Join(base, "Alice Example", "gate data")
	set := selfSet{roots: []string{root}, dirs: []string{root}}
	cwd := filepath.Join(base, "work tree")
	for name, args := range callsNaming(filepath.Join(base, "Alice Example", "gate data", "adm*")) {
		t.Run("tail glob "+name, func(t *testing.T) {
			if !touches(t, cwd, set, "Bash", args) {
				t.Fatalf("not rejected: %v", args)
			}
		})
	}
	for name, args := range callsNaming(filepath.Join(base, "Alice Example", "gat?*")) {
		t.Run("directory glob "+name, func(t *testing.T) {
			if !touches(t, cwd, selfSet{dirs: []string{root}}, "Bash", args) {
				t.Fatalf("a glob over a protected directory was allowed: %v", args)
			}
		})
	}
	for name, args := range callsNaming(filepath.Join(base, "Alice Example", "other*")) {
		t.Run("unrelated glob "+name, func(t *testing.T) {
			if touches(t, cwd, set, "Bash", args) {
				t.Fatalf("an unrelated glob was rejected: %v", args)
			}
		})
	}
}
