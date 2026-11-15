package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate/exempt"
	"github.com/rfizzle/astimate/internal/metrics"
)

// TestLoadTargetExemptionsFile checks that under the embedded default,
// resolved by LoadTarget or passed in as an MCP server resolves it, the
// target's configuration carries the exemptions file of the module root,
// read from below the package the target names, and that a configuration
// built by Parse reads none.
func TestLoadTargetExemptionsFile(t *testing.T) {
	t.Parallel()

	// This package's directory holds no astimate.yaml, so the default
	// resolution falls back to the embedded default.
	if _, err := os.Stat(config.FileName); err == nil {
		t.Fatalf("%s exists in the test's working directory", config.FileName)
	}
	embedded, source, err := config.Resolve("")
	if err != nil || source != config.SourceEmbedded {
		t.Fatalf("config.Resolve(\"\") = %q, %v; want the embedded default", source, err)
	}
	// The subtests run in parallel, after this function returns.
	t.Cleanup(func() {
		if len(embedded.Exemptions) != 0 {
			t.Errorf("LoadTarget changed the configuration passed in: %+v", embedded.Exemptions)
		}
	})
	parsed, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	want := []exempt.Entry{{Package: "sub", Metric: "globals", Reason: "kept on purpose", Source: config.ExemptionsFileName}}
	tests := []struct {
		name    string
		opts    TargetOptions
		file    string
		want    []exempt.Entry
		wantErr string
	}{
		{name: "resolved here", file: "exemptions:\n  - package: sub\n    metric: globals\n    reason: kept on purpose\n", want: want},
		{name: "passed in", opts: TargetOptions{Config: embedded},
			file: "exemptions:\n  - package: sub\n    metric: globals\n    reason: kept on purpose\n", want: want},
		{name: "parsed configuration reads none", opts: TargetOptions{Config: parsed},
			file: "exemptions:\n  - package: sub\n    metric: globals\n    reason: kept on purpose\n"},
		{name: "no file"},
		{name: "invalid file", file: "exemptions:\n  - package: sub\n    metric: globals\n",
			wantErr: config.ExemptionsFileName + ": exemptions[0]: reason is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for name, data := range map[string]string{
				"go.mod":          "module example.com/m\n\ngo 1.22\n",
				"sub/sub.go":      "package sub\n",
				"sub/nested/x.go": "package nested\n",
			} {
				p := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.file != "" {
				if err := os.WriteFile(filepath.Join(root, config.ExemptionsFileName), []byte(tt.file), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			tg, err := LoadTarget(filepath.Join(root, "sub", "nested"), tt.opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("LoadTarget error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(tg.Cfg.Exemptions, tt.want) {
				t.Errorf("Cfg.Exemptions = %+v, want %+v", tg.Cfg.Exemptions, tt.want)
			}
		})
	}
}

// TestCheckExemptionsFileSelectsNothing checks that the exemptions file is
// configuration, not source: adding it, in the module root or beside a
// package, selects no package of a check against the base commit, and
// every row of a check of all packages measures the same with and without
// it.
func TestCheckExemptionsFileSelectsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	dir := excludeRepo(t)
	const file = "exemptions:\n  - package: p\n    metric: globals\n    reason: kept on purpose\n"
	measure := func(t *testing.T) map[string]metrics.RawMetrics {
		t.Helper()
		tg, err := LoadTarget(dir, TargetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		changed, failed, err := Check(t.Context(), tg, CheckOptions{Base: "base"})
		if err != nil || len(failed) != 0 {
			t.Fatalf("Check = %v, %v", failed, err)
		}
		if len(changed.Packages) != 0 || len(changed.Deleted) != 0 {
			t.Errorf("Check against base selected %d packages and deleted %v, want none", len(changed.Packages), changed.Deleted)
		}
		all, failed, err := Check(t.Context(), tg, CheckOptions{Base: "base", All: true})
		if err != nil || len(failed) != 0 {
			t.Fatalf("Check --all = %v, %v", failed, err)
		}
		out := make(map[string]metrics.RawMetrics, len(all.Packages))
		for i := range all.Packages {
			out[all.Packages[i].Report.PackagePath] = all.Packages[i].Report.Metrics
		}
		return out
	}
	before := measure(t)
	writeFiles(t, dir, map[string]string{config.ExemptionsFileName: file, "p/" + config.ExemptionsFileName: file})
	if tg, err := LoadTarget(dir, TargetOptions{}); err != nil || len(tg.Cfg.Exemptions) != 1 {
		t.Fatalf("LoadTarget = %v, %v; want the module root's exemption", tg, err)
	}
	if after := measure(t); len(after) == 0 || !reflect.DeepEqual(after, before) {
		t.Errorf("metrics with the exemptions file = %+v, want %+v", after, before)
	}
}
