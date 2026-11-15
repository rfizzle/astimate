package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate/exempt"
)

// configExemption is the exemption the tests' configuration file carries
// itself, and fileEntry one entry of an exemptions file.
const (
	configExemption = "exemptions:\n  - package: internal/kit\n    metric: globals\n    reason: in the config\n"
	fileEntry       = "  - package: internal/local\n    metric: sloc\n    reason: in the file\n    expires: 2026-12-31\n"
)

// fromConfig returns configExemption as parsed.
func fromConfig() exempt.Entry {
	return exempt.Entry{Package: "internal/kit", Metric: "globals", Reason: "in the config"}
}

// fromFile returns fileEntry as parsed, with the file as its source.
func fromFile() exempt.Entry {
	return exempt.Entry{Package: "internal/local", Metric: "sloc", Reason: "in the file", Expires: "2026-12-31",
		Source: ExemptionsFileName}
}

// writeFile writes data to name in dir, failing the test on error.
func writeFile(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolveExemptionsFile checks that the exemptions file beside a
// configuration file is read with it: absent or empty it adds nothing, and
// its entries follow the configuration's own, validated the same way, its
// errors naming the file.
func TestResolveExemptionsFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// config is the configuration's exemptions section; file the
		// exemptions file, not written when nil.
		config  string
		file    *string
		want    []exempt.Entry
		wantErr []string
	}{
		{name: "file absent", config: configExemption, want: []exempt.Entry{fromConfig()}},
		{name: "empty file", config: configExemption, file: new(""), want: []exempt.Entry{fromConfig()}},
		{name: "comments only", file: new("# nothing exempted yet\n")},
		{name: "empty list", file: new("exemptions: []\n")},
		{name: "null list", file: new("exemptions:\n")},
		{name: "one entry", file: new("exemptions:\n" + fileEntry), want: []exempt.Entry{fromFile()}},
		{name: "config first, then the file", config: configExemption, file: new("exemptions:\n" + fileEntry),
			want: []exempt.Entry{fromConfig(), fromFile()}},
		{name: "a metric only a languages override gates",
			file: new("exemptions:\n  - package: x\n    metric: fan_in\n    reason: hub\n"),
			want: []exempt.Entry{{Package: "x", Metric: "fan_in", Reason: "hub", Source: ExemptionsFileName}}},
		{name: "unknown top-level key names the file and the key",
			file:    new("exemptions:\n" + fileEntry + "thresholds:\n  - metric: sloc\n    kind: capacity\n    max: 5000\n"),
			wantErr: []string{ExemptionsFileName + ": thresholds: the file holds only exemptions; set thresholds in astimate.yaml"}},
		{name: "every unknown key is named", file: new("rebuild: {}\nchars_per_token: 4\n"),
			wantErr: []string{ExemptionsFileName + ": chars_per_token: ", ExemptionsFileName + ": rebuild: "}},
		{name: "not a mapping", file: new("- package: x\n"), wantErr: []string{ExemptionsFileName + ": want a mapping holding only exemptions"}},
		{name: "invalid entry names the file and its index", config: configExemption,
			file:    new("exemptions:\n" + fileEntry + "  - package: internal/y\n    metric: globals\n"),
			wantErr: []string{ExemptionsFileName + ": exemptions[1]: reason is required"}},
		{name: "ungated metric", file: new("exemptions:\n  - package: x\n    metric: globalz\n    reason: r\n"),
			wantErr: []string{ExemptionsFileName + `: exemptions[0]: metric "globalz": no threshold gates it`}},
		{name: "module-wide metric on a package",
			file:    new("exemptions:\n  - package: x\n    metric: dup_blocks_cross_pkg\n    reason: r\n"),
			wantErr: []string{ExemptionsFileName + `: exemptions[0]: metric "dup_blocks_cross_pkg" is gated on the module row only`}},
		{name: "unknown entry key", file: new("exemptions:\n  - package: x\n    metric: globals\n    reason: r\n    at: x.go:3\n"),
			wantErr: []string{"decoding ", ExemptionsFileName, "at"}},
	}
	langRule := replace(t, "languages:\n  typescript:\n    thresholds:\n",
		"languages:\n  typescript:\n    thresholds:\n      - metric: fan_in\n        kind: capacity\n        max: 30\n")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, FileName)
			writeFile(t, dir, FileName, langRule+"\n"+tt.config)
			if tt.file != nil {
				writeFile(t, dir, ExemptionsFileName, *tt.file)
			}
			cfg, source, err := resolveIn(cfgPath, t.TempDir())
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("resolveIn() = nil error, want %q", tt.wantErr)
				}
				for _, w := range tt.wantErr {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("resolveIn() error = %v, want it to contain %q", err, w)
					}
				}
				return
			}
			if err != nil || source != SourceFlag {
				t.Fatalf("resolveIn() = %q, %v; want the flagged file", source, err)
			}
			if !slices.Equal(cfg.Exemptions, tt.want) {
				t.Errorf("Exemptions = %+v, want %+v", cfg.Exemptions, tt.want)
			}
			for _, lang := range []string{"go", "typescript"} {
				if got := cfg.ForLanguage(lang).Exemptions; !slices.Equal(got, tt.want) {
					t.Errorf("ForLanguage(%q).Exemptions = %+v, want %+v", lang, got, tt.want)
				}
			}
			// The file was read with the configuration; ForModule reads
			// nothing more.
			if got, err := cfg.ForModule(t.TempDir()); err != nil || got != cfg {
				t.Errorf("ForModule() = %p, %v; want the same configuration", got, err)
			}
		})
	}
}

// TestResolveExemptionsFileLocation checks where the exemptions file is
// read from: beside the configuration file in use, wherever that is, and
// in the module root under the embedded default, once ForModule names it.
func TestResolveExemptionsFileLocation(t *testing.T) {
	t.Parallel()

	file := "exemptions:\n" + fileEntry
	t.Run("beside --config in another directory", func(t *testing.T) {
		t.Parallel()

		cfgDir, workDir := t.TempDir(), t.TempDir()
		writeFile(t, cfgDir, FileName, string(Default()))
		writeFile(t, cfgDir, ExemptionsFileName, file)
		// The working directory's file is not the configuration's.
		writeFile(t, workDir, ExemptionsFileName, "exemptions:\n  - package: wrong\n    metric: globals\n    reason: r\n")
		cfg, _, err := resolveIn(filepath.Join(cfgDir, FileName), workDir)
		if err != nil {
			t.Fatal(err)
		}
		if want := []exempt.Entry{fromFile()}; !slices.Equal(cfg.Exemptions, want) {
			t.Errorf("Exemptions = %+v, want %+v", cfg.Exemptions, want)
		}
	})
	t.Run("beside ./astimate.yaml", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		writeFile(t, dir, FileName, string(Default()))
		writeFile(t, dir, ExemptionsFileName, file)
		cfg, source, err := resolveIn("", dir)
		if err != nil || source != SourceWorkDir {
			t.Fatalf("resolveIn() = %q, %v; want ./astimate.yaml", source, err)
		}
		if want := []exempt.Entry{fromFile()}; !slices.Equal(cfg.Exemptions, want) {
			t.Errorf("Exemptions = %+v, want %+v", cfg.Exemptions, want)
		}
	})
	t.Run("embedded default reads the module root", func(t *testing.T) {
		t.Parallel()

		workDir, root := t.TempDir(), t.TempDir()
		// The working directory holds no configuration, so its exemptions
		// file is not read: the default sits in no directory.
		writeFile(t, workDir, ExemptionsFileName, "exemptions:\n  - package: wrong\n    metric: globals\n    reason: r\n")
		writeFile(t, root, ExemptionsFileName, file)
		cfg, source, err := resolveIn("", workDir)
		if err != nil || source != SourceEmbedded {
			t.Fatalf("resolveIn() = %q, %v; want the embedded default", source, err)
		}
		if len(cfg.Exemptions) != 0 {
			t.Fatalf("Exemptions before ForModule = %+v, want none", cfg.Exemptions)
		}
		got, err := cfg.ForModule(root)
		if err != nil {
			t.Fatal(err)
		}
		if want := []exempt.Entry{fromFile()}; !slices.Equal(got.Exemptions, want) {
			t.Errorf("ForModule().Exemptions = %+v, want %+v", got.Exemptions, want)
		}
		if len(cfg.Exemptions) != 0 {
			t.Errorf("ForModule changed the resolved configuration: %+v", cfg.Exemptions)
		}
		// The result has read its file; another module's is not added.
		if again, err := got.ForModule(workDir); err != nil || again != got {
			t.Errorf("ForModule() on its own result = %p, %v; want it unchanged", again, err)
		}
		// Each module gets its own file.
		if other, err := cfg.ForModule(t.TempDir()); err != nil || len(other.Exemptions) != 0 {
			t.Errorf("ForModule(module without the file) = %+v, %v; want no exemptions", other, err)
		}
		writeFile(t, root, ExemptionsFileName, "exemptions:\n  - package: x\n")
		if _, err := cfg.ForModule(root); err == nil || !strings.Contains(err.Error(), ExemptionsFileName+": exemptions[0]: metric is required") {
			t.Errorf("ForModule(invalid file) error = %v, want it to name the file and the entry", err)
		}
	})
	t.Run("Parse and Load read no exemptions file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		writeFile(t, dir, FileName, string(Default()))
		writeFile(t, dir, ExemptionsFileName, file)
		loaded, err := Load(filepath.Join(dir, FileName))
		if err != nil {
			t.Fatal(err)
		}
		for name, cfg := range map[string]*Config{"Load": loaded, "Parse": mustParse(t, Default())} {
			got, err := cfg.ForModule(dir)
			if err != nil || got != cfg || len(got.Exemptions) != 0 {
				t.Errorf("%s then ForModule = %+v, %v; want the configuration unchanged", name, got.Exemptions, err)
			}
		}
	})
}

// mustParse parses data, failing the test on error.
func mustParse(t *testing.T, data []byte) *Config {
	t.Helper()
	cfg, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
