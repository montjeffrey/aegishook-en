package core

import (
	"os"
	"path/filepath"
	"testing"
)

// scopeLayout is a project with an allowed "inside" directory, a sibling "outside"
// directory that the scope does not allow, and an unrelated project.
type scopeLayout struct{ project, inside, outside, unrelated string }

func makeScopeLayout(t *testing.T, parent string) scopeLayout {
	t.Helper()
	l := scopeLayout{
		project:   filepath.Join(parent, "alice project"),
		unrelated: filepath.Join(parent, "someone else"),
	}
	l.inside, l.outside = filepath.Join(l.project, "inside"), filepath.Join(l.project, "outside")
	for _, d := range []string{l.inside, l.outside, l.unrelated} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func scopeRead(path string) ReviewInput {
	return ReviewInput{ToolName: "Read", Arguments: map[string]any{"file_path": filepath.Join(path, "f.txt")}}
}

// assertScopeEnforced checks one spelling combination. project and cwd name the same
// project in two spellings; allowed, inside and outside are spelled however the
// caller likes.
func assertScopeEnforced(t *testing.T, project, cwd, allowed, inside, outside, unrelated string) {
	t.Helper()
	scopes := []Scope{{Project: project, Paths: []string{allowed}}}
	if d := checkScope(scopeRead(inside), cwd, scopes); d != nil {
		t.Errorf("a path inside the scope was rejected: %s / %s", d.Decision, d.RuleID)
	}
	if d := checkScope(scopeRead(outside), cwd, scopes); d == nil || d.RuleID != "SCOPE_PATH" {
		t.Errorf("a path outside the scope was not rejected (scope skipped?): %+v", d)
	}
	// A scope for some other project does not apply to this one.
	other := []Scope{{Project: unrelated, Paths: []string{allowed}}}
	if d := checkScope(scopeRead(outside), cwd, other); d != nil {
		t.Errorf("an unrelated project's scope was applied: %+v", d)
	}
}

// A project root reached through a symlink must be the same project as its target.
func TestScopeAppliesWhenProjectRootIsASymlink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real parent")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	l := makeScopeLayout(t, real)
	link := filepath.Join(base, "link parent")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	lp := scopeLayout{
		project:   filepath.Join(link, "alice project"),
		inside:    filepath.Join(link, "alice project", "inside"),
		outside:   filepath.Join(link, "alice project", "outside"),
		unrelated: filepath.Join(link, "someone else"),
	}
	t.Run("project via link, cwd real", func(t *testing.T) {
		assertScopeEnforced(t, lp.project, l.project, l.inside, l.inside, l.outside, l.unrelated)
	})
	t.Run("project real, cwd via link", func(t *testing.T) {
		assertScopeEnforced(t, l.project, lp.project, l.inside, lp.inside, lp.outside, l.unrelated)
	})
	t.Run("allowed root via link, paths real", func(t *testing.T) {
		assertScopeEnforced(t, l.project, l.project, lp.inside, l.inside, l.outside, l.unrelated)
	})
	t.Run("everything via link, cwd real", func(t *testing.T) {
		assertScopeEnforced(t, lp.project, l.project, lp.inside, lp.inside, lp.outside, lp.unrelated)
	})
}
