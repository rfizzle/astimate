package baseline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
)

func TestFileNamed(t *testing.T) {
	t.Parallel()

	pkgs := map[string]metrics.RawMetrics{"example.com/m/a": {}, "example.com/m/b": {}, metrics.ModuleRowID: {}}
	named := map[string]Named{
		"example.com/m/a": {DupBlocks: []uint64{0xff, 0xfedcba9876543210}, UntestedExports: []string{"Join", "T.Run"},
			Exports: []metrics.Symbol{{Kind: "func", Name: "Join"}, {Kind: "type", Name: "T"}, {Kind: "func", Receiver: "T", Name: "Run"}}},
		"example.com/m/b": {},
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := WriteContents(path, Contents{Ref: "abc", ModulePath: "example.com/m", Packages: pkgs, Named: named}); err != nil {
		t.Fatalf("WriteContents: %v", err)
	}
	b, err := FromFile(path)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}
	if got, _ := b.Row("a"); !got.HasNamed || !reflect.DeepEqual(got.Named, named["example.com/m/a"]) {
		t.Errorf("Row(a) named = %+v, %v, want %+v", got.Named, got.HasNamed, named["example.com/m/a"])
	}
	if got, _ := b.Row("b"); !got.HasNamed || len(got.Named.DupBlocks) != 0 || len(got.Named.UntestedExports) != 0 {
		t.Errorf("Row(b) named = %+v, %v, want recorded and empty", got.Named, got.HasNamed)
	}
	if got, _ := b.Row(metrics.ModuleRowID); got.HasNamed {
		t.Errorf("module row named = %+v, want none", got.Named)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		PackageRows map[string]map[string]any `json:"package_rows"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if got := raw.PackageRows["example.com/m/a"]["dup_blocks"]; !reflect.DeepEqual(got, []any{"00000000000000ff", "fedcba9876543210"}) {
		t.Errorf("dup_blocks recorded as %v, want 16 hex digits each", got)
	}
	if got := raw.PackageRows["example.com/m/b"]["untested_exports"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("b's untested_exports recorded as %v, want an empty list", got)
	}
	wantExports := []any{map[string]any{"kind": "func", "name": "Join"}, map[string]any{"kind": "type", "name": "T"},
		map[string]any{"kind": "func", "receiver": "T", "name": "Run"}}
	if got := raw.PackageRows["example.com/m/a"]["exports"]; !reflect.DeepEqual(got, wantExports) {
		t.Errorf("a's exports recorded as %v, want %v", got, wantExports)
	}

	t.Run("file without details", func(t *testing.T) {
		t.Parallel()
		old := filepath.Join(t.TempDir(), "old.json")
		if err := Write(old, "abc", "example.com/m", "est", pkgs); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(old)
		if err != nil {
			t.Fatal(err)
		}
		if s := string(data); strings.Contains(s, `"dup_blocks": [`) || strings.Contains(s, `"untested_exports": [`) {
			t.Errorf("file recording no details lists blocks or exports:\n%s", data)
		}
		b, err := FromFile(old)
		if err != nil {
			t.Fatal(err)
		}
		if r, ok := b.Row("a"); !ok || r.HasNamed {
			t.Errorf("Row(a) = %+v, %v, want a row recording no details", r, ok)
		}
	})
	t.Run("file before exports and signatures", func(t *testing.T) {
		t.Parallel()
		old := filepath.Join(t.TempDir(), "old.json")
		body := `{"ref": "", "module_path": "example.com/m", "packages": {"example.com/m/a": {}},
			"functions": {"example.com/m/a": [{"name": "F", "fingerprint": "00000000000000ff", "cognitive": 1}]},
			"package_rows": {"example.com/m/a": {"package_path": "a", "dup_blocks": [], "untested_exports": ["F"]}}}`
		if err := os.WriteFile(old, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		b, err := FromFile(old)
		if err != nil {
			t.Fatalf("FromFile: %v", err)
		}
		r, _ := b.Row("a")
		if !r.HasNamed || r.Named.Exports != nil || !r.HasFunctions || r.Functions[0].Signature != 0 {
			t.Errorf("Row(a) = %+v, want details and functions recorded, with no exports and no signature", r)
		}
	})
	t.Run("bad fingerprint", func(t *testing.T) {
		t.Parallel()
		bad := filepath.Join(t.TempDir(), "bad.json")
		body := `{"ref": "", "module_path": "example.com/m", "packages": {"example.com/m/a": {}},
			"package_rows": {"example.com/m/a": {"package_path": "a", "dup_blocks": ["xyz"]}}}`
		if err := os.WriteFile(bad, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := FromFile(bad); err == nil {
			t.Error("FromFile accepted a malformed duplicate block fingerprint")
		}
	})
}

func TestCollectNamedWithoutDetailer(t *testing.T) {
	t.Parallel()

	pkgs := map[string]metrics.RawMetrics{"example.com/m/a": {}}
	named, err := CollectNamed(t.Context(), &stubExtractor{}, &metrics.ModuleContext{}, pkgs)
	if err != nil || named != nil {
		t.Errorf("CollectNamed = %v, %v, want nil and no error for an extractor without Detailer", named, err)
	}
}

// dupSource returns two copies of a loop, a duplicate block, as functions
// named prefix1 and prefix2.
func dupSource(pkg, prefix string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n")
	for _, n := range []string{"1", "2"} {
		b.WriteString("\n// " + prefix + n + " folds xs.\nfunc " + prefix + n + "(xs []int) int {\n\ttotal := 0\n" +
			"\tfor i, x := range xs {\n\t\tif x > i {\n\t\t\ttotal += x * 2\n\t\t} else {\n\t\t\ttotal -= x\n\t\t}\n\t}\n\treturn total\n}\n")
	}
	return b.String()
}

func TestFromGitRecordsNamed(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	r := newRepo(t, "master")
	r.write("go.mod", "module example.com/m\n\ngo 1.27\n")
	r.write("a/a.go", dupSource("a", "Fold"))
	first := r.commit("first")

	b, err := FromGit(context.Background(), r.dir, first, golang.New(), golang.ModulePath, "est")
	if err != nil {
		t.Fatalf("FromGit: %v", err)
	}
	row, _ := b.Row("a")
	if !row.HasNamed || len(row.Named.DupBlocks) != 2 || row.Named.DupBlocks[0] == 0 ||
		!slices.Equal(row.Named.UntestedExports, []string{"Fold1", "Fold2"}) {
		t.Errorf("Row(a) named = %+v, %v, want the two anchors of one block and Fold1, Fold2 untested", row.Named, row.HasNamed)
	}
	wantExports := []metrics.Symbol{{Kind: "func", Name: "Fold1"}, {Kind: "func", Name: "Fold2"}}
	if !reflect.DeepEqual(row.Named.Exports, wantExports) {
		t.Errorf("Row(a) exports = %+v, want %+v without positions", row.Named.Exports, wantExports)
	}
	for _, f := range row.Functions {
		if f.Signature == 0 {
			t.Errorf("Row(a) function %s has no signature", f.Name)
		}
	}
}

func TestSeededNamed(t *testing.T) {
	t.Parallel()

	base := rowsBaseline(map[string]Row{
		"a": {Named: Named{DupBlocks: []uint64{9, 3}, UntestedExports: []string{"Z"}, Exports: []metrics.Symbol{{Kind: "func", Name: "Z"}}}, HasNamed: true},
		"b": {Named: Named{DupBlocks: []uint64{5, 9}, UntestedExports: []string{"A"}, Exports: []metrics.Symbol{{Kind: "func", Name: "A"}}}, HasNamed: true},
		"c": {},
	})
	s := Seeded(base, Renames{"sub": {{Dir: "a", Files: 1}, {Dir: "b", Files: 1}}, "new": {{Dir: "c", Files: 1}}}, nil)
	sub, _ := s.Row("sub")
	want := Named{DupBlocks: []uint64{3, 5, 9}, UntestedExports: []string{"A", "Z"},
		Exports: []metrics.Symbol{{Kind: "func", Name: "A"}, {Kind: "func", Name: "Z"}}}
	if !sub.HasNamed || !reflect.DeepEqual(sub.Named, want) {
		t.Errorf("Row(sub) named = %+v, %v, want every source's, sorted and distinct: %+v", sub.Named, sub.HasNamed, want)
	}
	if r, _ := s.Row("new"); r.HasNamed {
		t.Errorf("Row(new) named = %+v, want none: its source recorded none", r.Named)
	}
}
