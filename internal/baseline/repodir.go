package baseline

import (
	"context"
	"path/filepath"
)

// RepoDir returns root's directory relative to the top level of the git
// working tree holding it, in slash form: "" when root is the top level,
// for example "services/api" when the module lives below it. Renderers
// prefix module-relative paths with it so they are relative to the
// repository, as GitHub annotations must be. When root is in no git
// repository, or git cannot say, it returns "" and the module root stays
// the prefix: a check outside git has no repository to be relative to.
func RepoDir(ctx context.Context, root string) string {
	rel, err := repoRelative(ctx, root)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}
