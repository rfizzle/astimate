package baseline

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestTreeRelative(t *testing.T) {
	t.Parallel()

	tree := t.TempDir()
	resolved, err := filepath.EvalSymlinks(tree)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join("bad", "bad.go")
	cause := errors.New("cause")
	tests := []struct {
		name string
		err  error
		tree string
		want string
	}{
		{name: "file below tree", err: fmt.Errorf("%s: %w", filepath.Join(tree, file), cause),
			tree: tree, want: file + ": cause"},
		{name: "resolved form", err: fmt.Errorf("%s: %w", filepath.Join(resolved, file), cause),
			tree: tree, want: file + ": cause"},
		{name: "tree itself", err: fmt.Errorf("loading %s: %w", tree, cause), tree: tree, want: "loading .: cause"},
		{name: "no mention", err: cause, tree: tree, want: "cause"},
		{name: "empty tree", err: fmt.Errorf("%s: %w", filepath.Join(tree, file), cause),
			tree: "", want: filepath.Join(tree, file) + ": cause"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := TreeRelative(tt.err, tt.tree)
			if got.Error() != tt.want {
				t.Errorf("TreeRelative = %q, want %q", got.Error(), tt.want)
			}
			if !errors.Is(got, cause) {
				t.Error("result does not unwrap to the cause")
			}
		})
	}
	if TreeRelative(nil, tree) != nil {
		t.Error("TreeRelative(nil) is not nil")
	}
}
