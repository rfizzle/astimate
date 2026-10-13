package baseline

import (
	"context"

	"github.com/rfizzle/astimate/internal/baseline/internal/diff"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Change is the set of packages a change touches relative to a merge-base
// (SPEC.md 8.4); see diff.Change.
type Change = diff.Change

// Head is the head side ChangedPackages and FindRenames compare with a
// merge-base: the working tree when zero, or the index; see diff.Head.
type Head = diff.Head

// RenameSource is a baseline package that files of a head package were
// renamed from; see diff.RenameSource.
type RenameSource = diff.RenameSource

// Renames maps each head package that non-test source files were renamed
// into to the packages they came from; see diff.Renames.
type Renames = diff.Renames

// ChangedPackages returns the packages of the module at root whose source
// files differ between the commit mergeBase and head, with sc saying which
// files are source and which package each belongs to
// (diff.ChangedPackages).
func ChangedPackages(ctx context.Context, root, mergeBase string, sc metrics.SourceClassifier, head Head) (Change, error) {
	return diff.ChangedPackages(ctx, root, mergeBase, sc, head)
}

// FindRenames returns the renames git detects between the commit mergeBase
// and head in the module at root, per head package, that Seeded seeds a
// moved package's row from (diff.FindRenames).
func FindRenames(ctx context.Context, root, mergeBase string, sc metrics.SourceClassifier, head Head) (Renames, error) {
	return diff.FindRenames(ctx, root, mergeBase, sc, head)
}
