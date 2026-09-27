package baseline

import (
	"path/filepath"
	"strings"
)

// TreeRelative returns err with every mention of tree, the module root in
// a temporary copy such as StagedTree returns or FromGit checks out,
// rewritten relative to it: a path below tree loses the prefix and tree
// itself reads ".". The copy is removed before anyone reads the error and
// its name means nothing to the user, while a module-relative path reads
// as report paths do. tree's symlink-resolved form, which a loader may
// report instead, is rewritten too, so call it while tree still exists.
// The result unwraps to err. A nil err, an empty tree, or text that does
// not mention tree returns err unchanged.
func TreeRelative(err error, tree string) error {
	if err == nil || tree == "" {
		return err
	}
	prefixes := []string{filepath.Clean(tree)}
	if resolved, rerr := filepath.EvalSymlinks(tree); rerr == nil && resolved != prefixes[0] {
		// Rewrite the longer form first: one may end with the other, as
		// /private/var/x ends with /var/x.
		if len(resolved) > len(prefixes[0]) {
			prefixes = []string{resolved, prefixes[0]}
		} else {
			prefixes = append(prefixes, resolved)
		}
	}
	msg := err.Error()
	out := msg
	for _, p := range prefixes {
		out = strings.ReplaceAll(out, p+string(filepath.Separator), "")
		out = strings.ReplaceAll(out, p, ".")
	}
	if out == msg {
		return err
	}
	return &treeError{msg: out, err: err}
}

// treeError is an error whose text TreeRelative rewrote.
type treeError struct {
	msg string
	err error
}

// Error returns the rewritten text.
func (e *treeError) Error() string { return e.msg }

// Unwrap returns the original error.
func (e *treeError) Unwrap() error { return e.err }
