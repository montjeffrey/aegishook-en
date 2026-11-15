package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The project root as configured and the cwd as the client reports it can be an 8.3
// short name and the long name of the same directory.
func TestScopeAppliesAcrossShortAndLongNames(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "A Long Directory Name For Short Names")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	l := makeScopeLayout(t, parent)
	short := scopeLayout{
		project:   shortSpelling(t, l.project),
		inside:    shortSpelling(t, l.inside),
		outside:   shortSpelling(t, l.outside),
		unrelated: shortSpelling(t, l.unrelated),
	}
	if strings.EqualFold(short.project, l.project) || strings.EqualFold(short.inside, l.inside) {
		t.Skip("8.3 short names are disabled on this volume")
	}
	t.Run("project short, cwd long", func(t *testing.T) {
		assertScopeEnforced(t, short.project, l.project, l.inside, l.inside, l.outside, l.unrelated)
	})
	t.Run("project long, cwd short", func(t *testing.T) {
		assertScopeEnforced(t, l.project, short.project, l.inside, short.inside, short.outside, l.unrelated)
	})
	t.Run("allowed root short, path long", func(t *testing.T) {
		assertScopeEnforced(t, l.project, l.project, short.inside, l.inside, l.outside, l.unrelated)
	})
	t.Run("allowed root long, path short", func(t *testing.T) {
		assertScopeEnforced(t, short.project, short.project, l.inside, short.inside, short.outside, short.unrelated)
	})
}
