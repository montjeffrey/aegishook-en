package core

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// shortSpelling returns the 8.3 spelling of an existing path.
func shortSpelling(t *testing.T, p string) string {
	t.Helper()
	in, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 1024)
	n, err := syscall.GetShortPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		t.Skip("cannot read the 8.3 name:", err)
	}
	return syscall.UTF16ToString(buf[:n])
}

// A Windows runner's temp directory is an 8.3 path (C:\Users\RUNNER~1\...), while
// resolving a relative path expands it to the long name. The protected root and the
// candidate must be compared in one form, whichever way each is spelled.
func TestSelfProtectComparesShortAndLongSpellings(t *testing.T) {
	isolatedHome(t)
	parent := filepath.Join(t.TempDir(), "A Long Directory Name For Short Names")
	dataLong := filepath.Join(parent, "gate data")
	workLong := filepath.Join(parent, "work tree")
	for _, d := range []string{dataLong, workLong} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dataLong, "admin.token"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	parentShort := shortSpelling(t, parent)
	if strings.EqualFold(parentShort, parent) {
		t.Skip("8.3 short names are disabled on this volume")
	}
	dataShort, workShort := shortSpelling(t, dataLong), shortSpelling(t, workLong)
	if strings.EqualFold(dataShort, dataLong) {
		t.Skip("8.3 short names are disabled on this volume")
	}
	secret := func(dir string) string { return filepath.Join(dir, "admin.token") }
	rel, err := filepath.Rel(workShort, secret(dataShort))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, root, cwd, arg string
	}{
		{"root short, path long", dataShort, workLong, secret(dataLong)},
		{"root long, path short", dataLong, workLong, secret(dataShort)},
		{"root short, relative walk from short cwd", dataShort, workShort, rel},
		{"root long, relative walk from short cwd", dataLong, workShort, rel},
		{"root short, relative walk from long cwd", dataShort, workLong, rel},
		{"root short, mixed case long path", dataShort, workLong, strings.ToUpper(secret(dataLong))},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := ReviewInput{ToolName: "Read", Arguments: map[string]any{"file_path": c.arg}}
			if !selfTouches(in, c.cwd, selfSet{roots: []string{c.root}}, policyPath, true) {
				t.Fatalf("not rejected: root %q cwd %q arg %q", c.root, c.cwd, c.arg)
			}
		})
	}
	// Nothing else under the same parent is claimed.
	in := ReviewInput{ToolName: "Read", Arguments: map[string]any{"file_path": filepath.Join(workShort, "notes.txt")}}
	if selfTouches(in, workShort, selfSet{roots: []string{dataShort}}, policyPath, true) {
		t.Fatal("a sibling directory was rejected")
	}
}

// A path with spaces is not one token, so it has to be resolved as a whole. Mixed
// spellings (one directory short, the next long) are neither the configured root
// nor its resolved form, so only the filesystem can tell they are the same place.
func TestSelfProtectResolvesMixedShortAndLongSpellingsWithSpaces(t *testing.T) {
	isolatedHome(t)
	outer := filepath.Join(t.TempDir(), "Alice Example Profile")
	inner := filepath.Join(outer, "My Data Folder")
	gate := filepath.Join(inner, "gate")
	cwd := filepath.Join(outer, "work tree")
	for _, d := range []string{gate, cwd} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(gate, "admin.token"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	outerShort, innerShort := shortSpelling(t, outer), shortSpelling(t, inner)
	if strings.EqualFold(outerShort, outer) || strings.EqualFold(innerShort, inner) {
		t.Skip("8.3 short names are disabled on this volume")
	}
	innerName := filepath.Base(innerShort) // the short name of the inner directory alone
	variants := map[string]string{
		"outer short, inner long": filepath.Join(outerShort, "My Data Folder"),
		"outer long, inner short": filepath.Join(outer, innerName),
		"both short":              innerShort,
	}
	for _, root := range []string{gate, filepath.Join(innerShort, "gate")} {
		for name, dir := range variants {
			cand := filepath.Join(dir, "gate", "admin.token")
			for form, args := range callsNaming(cand) {
				t.Run(name+" "+form, func(t *testing.T) {
					if !touches(t, cwd, selfSet{roots: []string{root}}, "Bash", args) {
						t.Fatalf("not rejected: root %q arg %v", root, args)
					}
				})
			}
			t.Run(name+" directory", func(t *testing.T) {
				if !touches(t, cwd, selfSet{dirs: []string{root}}, "Read", map[string]any{"file_path": filepath.Join(dir, "gate")}) {
					t.Fatalf("protected directory not rejected: root %q dir %q", root, dir)
				}
			})
		}
	}
	other := filepath.Join(outerShort, "My Data Folder", "notes.txt")
	if touches(t, cwd, selfSet{roots: []string{gate}}, "Read", map[string]any{"file_path": other}) {
		t.Fatal("a sibling file was rejected")
	}
}
