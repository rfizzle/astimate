package baseline

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/lang/typescript"
	"github.com/rfizzle/astimate/internal/metrics"
)

func TestParseNameStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		out     string
		want    []changedPath
		wantErr bool
	}{
		{name: "empty", out: "", want: []changedPath{}},
		{
			name: "added, modified, deleted, type changed and unmerged",
			out:  "A\x00a/new.ts\x00M\x00b/b.ts\x00D\x00c/types.d.ts\x00T\x00d/link.ts\x00U\x00e/e.ts\x00",
			want: []changedPath{
				{path: "a/new.ts"},
				{path: "b/b.ts"},
				{path: "c/types.d.ts", deleted: true},
				{path: "d/link.ts"},
				{path: "e/e.ts"},
			},
		},
		{name: "path with a space and a newline", out: "D\x00a b/x\ny.ts\x00", want: []changedPath{{path: "a b/x\ny.ts", deleted: true}}},
		{name: "missing path", out: "M\x00a.ts\x00D\x00", wantErr: true},
		{name: "rename status", out: "R100\x00old.ts\x00", wantErr: true},
		{name: "copy status", out: "C100\x00old.ts\x00", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseNameStatus(tt.out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseNameStatus(%q) error = %v, want error %v", tt.out, err, tt.wantErr)
			}
			if !tt.wantErr && !slices.Equal(got, tt.want) {
				t.Errorf("parseNameStatus(%q) = %+v, want %+v", tt.out, got, tt.want)
			}
		})
	}
}

func TestChangedPackagesRemovedContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		change       func(r *testRepo)
		wantPkgs     []string
		wantDeleted  []string
		wantContract []string
		wantRemoved  []string
	}{
		{
			name:         "declaration file deleted from a surviving package",
			change:       func(r *testRepo) { r.git("rm", "-q", "a/types.d.ts") },
			wantPkgs:     []string{"a"},
			wantContract: []string{"a"},
			wantRemoved:  []string{"a"},
		},
		{
			name: "declaration file deleted from the working tree only",
			change: func(r *testRepo) {
				if err := os.Remove(filepath.Join(r.dir, "a", "types.d.ts")); err != nil {
					r.t.Fatal(err)
				}
			},
			wantPkgs:     []string{"a"},
			wantContract: []string{"a"},
			wantRemoved:  []string{"a"},
		},
		{
			name: "package holding a declaration file deleted",
			change: func(r *testRepo) {
				r.git("rm", "-q", "-r", "a")
				r.commit("drop a")
			},
			wantDeleted: []string{"a"},
			wantRemoved: []string{"a"},
		},
		{
			name:         "modified declaration file is not removed",
			change:       func(r *testRepo) { r.write("a/types.d.ts", "export type A = string;\n") },
			wantPkgs:     []string{"a"},
			wantContract: []string{"a"},
		},
		{
			name:         "untracked declaration file is an addition",
			change:       func(r *testRepo) { r.write("b/types.d.ts", "export type B = string;\n") },
			wantPkgs:     []string{"b"},
			wantContract: []string{"b"},
		},
		{
			name:        "deleted source file is not a contract",
			change:      func(r *testRepo) { r.git("rm", "-q", "b/b.ts") },
			wantDeleted: []string{"b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, base := newTSChangeRepo(t)
			tt.change(r)
			got, err := ChangedPackages(context.Background(), r.dir, base, typescript.New(), Head{})
			if err != nil {
				t.Fatalf("ChangedPackages: %v", err)
			}
			if !slices.Equal(got.Packages, tt.wantPkgs) {
				t.Errorf("Packages = %q, want %q", got.Packages, tt.wantPkgs)
			}
			if !slices.Equal(got.Deleted, tt.wantDeleted) {
				t.Errorf("Deleted = %q, want %q", got.Deleted, tt.wantDeleted)
			}
			if !slices.Equal(got.Contract, tt.wantContract) {
				t.Errorf("Contract = %q, want %q", got.Contract, tt.wantContract)
			}
			if !slices.Equal(got.RemovedContract, tt.wantRemoved) {
				t.Errorf("RemovedContract = %q, want %q", got.RemovedContract, tt.wantRemoved)
			}
		})
	}
}

func TestChangedPackagesRemovedContractStaged(t *testing.T) {
	t.Parallel()
	r, base := newTSChangeRepo(t)
	r.git("rm", "-q", "--cached", "a/types.d.ts")

	tree, remove, err := StagedTree(context.Background(), r.dir, "")
	if err != nil {
		t.Fatalf("StagedTree: %v", err)
	}
	defer remove()
	got, err := ChangedPackages(context.Background(), r.dir, base, typescript.New(), Head{Staged: true, Tree: tree})
	if err != nil {
		t.Fatalf("ChangedPackages: %v", err)
	}
	if want := []string{"a"}; !slices.Equal(got.RemovedContract, want) {
		t.Errorf("RemovedContract = %q, want %q for a declaration file removed from the index", got.RemovedContract, want)
	}
}

func TestFromGitRecordsImporters(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	r := newRepo(t, "master")
	r.write("go.mod", "module example.com/m\n\ngo 1.27\n")
	r.write("a/a.go", "package a\n\nimport _ \"example.com/m/b\"\n")
	r.write("b/b.go", "package b\n")
	first := r.commit("first")
	r.write("a/a.go", "package a\n")
	r.commit("second")

	b, err := FromGit(context.Background(), r.dir, first, golang.New(), "example.com/m", "est")
	if err != nil {
		t.Fatalf("FromGit: %v", err)
	}
	if got, ok := b.Importers("example.com/m/b"); !ok || !slices.Equal(got, []string{"example.com/m/a"}) {
		t.Errorf("Importers(b) = %q, %v, want a from the first commit", got, ok)
	}
	if got, ok := b.Importers("example.com/m/a"); !ok || len(got) != 0 {
		t.Errorf("Importers(a) = %q, %v, want none and recorded", got, ok)
	}
	if got, ok := b.Importers("example.com/m/gone"); !ok || len(got) != 0 {
		t.Errorf("Importers(gone) = %q, %v, want none and recorded", got, ok)
	}
}

func TestCollectImportersWithoutLister(t *testing.T) {
	t.Parallel()

	pkgs := map[string]metrics.RawMetrics{"example.com/m/a": {}}
	importers, ok, err := CollectImporters(t.Context(), &stubExtractor{}, &metrics.ModuleContext{}, pkgs)
	if err != nil || ok || importers != nil {
		t.Errorf("CollectImporters = %v, %v, %v, want nil, false and no error for an extractor without ImporterLister",
			importers, ok, err)
	}
}

func TestFileBaselineRecordsNoImporters(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := Write(path, "abc123", "example.com/m", "est", map[string]metrics.RawMetrics{"example.com/m/a": {}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	b, err := FromFile(path)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}
	if got, ok := b.Importers("example.com/m/a"); ok || got != nil {
		t.Errorf("Importers(a) = %q, %v, want nil and false for a file baseline", got, ok)
	}
}
