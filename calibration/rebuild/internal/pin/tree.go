package pin

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/selection"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/stub"
)

// Listed is one package go list reports.
type Listed struct {
	// ImportPath and Name are the package's import path and name.
	ImportPath, Name string
	// Dir is the package directory relative to its module's root.
	Dir string
	// Err is the error go list reported for the package, if any.
	Err string
}

// ListPackages lists the packages matching pattern in the module at root,
// each with the error go list reported for it.
func ListPackages(ctx context.Context, root, pattern string, env []string) ([]Listed, error) {
	const format = `{{.ImportPath}}|{{.Name}}|{{with .Module}}{{.Path}}{{end}}|{{with .Error}}{{.Err}}{{end}}`
	out, err := RunGo(ctx, root, env, "list", "-e", "-f", format, pattern)
	if err != nil {
		return nil, fmt.Errorf("go list %s: %s", pattern, Tail(out))
	}
	var pkgs []Listed
	for line := range strings.Lines(out) {
		f := strings.SplitN(strings.TrimSpace(line), "|", 4)
		if len(f) != 4 {
			continue
		}
		pkgs = append(pkgs, Listed{ImportPath: f[0], Name: f[1], Dir: definition.ModRelDir(f[2], f[0]), Err: f[3]})
	}
	return pkgs, nil
}

// TreeMembers returns the module-relative directories, sorted, of the
// packages a tree at the module-relative directory dir stubs: its own and
// every package below it that is not a main package. go list leaves out
// testdata and directories starting with _ or a dot. It fails when go list
// reports an error for any package of the tree.
func TreeMembers(ctx context.Context, root, dir string, env []string) ([]string, error) {
	pkgs, err := ListPackages(ctx, root, definition.TreePattern(dir), env)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, p := range pkgs {
		if p.Err != "" {
			return nil, fmt.Errorf("go list: %s: %s", p.ImportPath, p.Err)
		}
		if p.Name != "main" {
			dirs = append(dirs, p.Dir)
		}
	}
	slices.Sort(dirs)
	return dirs, nil
}

// Mains returns the import paths of the main packages of the module
// module, cloned from repo at commit and checked like a candidate's.
func (k *Checker) Mains(ctx context.Context, module, repo, commit string) (map[string]bool, error) {
	root, err := k.clone(ctx, selection.Candidate{Row: selection.Row{Module: module, Commit: commit}, Repo: repo})
	if err != nil {
		return nil, err
	}
	pkgs, err := ListPackages(ctx, root, "./...", k.env)
	if err != nil {
		return nil, err
	}
	mains := map[string]bool{}
	for _, p := range pkgs {
		if p.Name == "main" {
			mains[p.ImportPath] = true
		}
	}
	return mains, nil
}

// CheckTree verifies tree candidate c at its pin as Check does a package:
// the packages below the tree's directory at the pin are c's members; no
// member has non-Go sources; its oracle has tests to run (the tree's own
// when a member has tests, else those of importers outside the tree); and
// the checks of verify pass with every member stubbed. The clone is
// restored to the pin afterwards.
func (k *Checker) CheckTree(ctx context.Context, c selection.Candidate) (definition.Experiment, error) {
	want := make([]string, len(c.Members))
	pkgs := make([]string, len(c.Members))
	for i, m := range c.Members {
		want[i], pkgs[i] = definition.ModRelDir(m.Row.Module, m.Row.Package), m.Row.Package
	}
	dir := definition.ModRelDir(c.Row.Module, c.Row.Package)
	return k.check(ctx, c, func(root string) (plan, error) {
		dirs, err := TreeMembers(ctx, root, dir, k.env)
		if err != nil {
			return plan{}, rejected("%s", err)
		}
		if !slices.Equal(dirs, want) {
			return plan{}, rejected("packages below %s at the pin %v are not the data's %v", dir, dirs, want)
		}
		return plan{
			dir: dir, dirs: dirs, own: definition.TreePattern(dir), targets: pkgs,
			skip:        func(p string) bool { return definition.InTree(dir, definition.ModRelDir(c.Row.Module, p)) },
			noImporters: "no importer outside the tree has tests",
			stub:        func() ([]stub.File, error) { return stub.Tree(ctx, root, dir, dirs, k.env) },
		}, nil
	})
}
