package metrics

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

type fakeExtractor struct {
	lang  string
	match bool
}

func (f fakeExtractor) Language() string                  { return f.lang }
func (f fakeExtractor) Detect(string) bool                { return f.match }
func (f fakeExtractor) Packages(string) ([]string, error) { return nil, nil }
func (f fakeExtractor) Extract(context.Context, *ModuleContext, string) (RawMetrics, error) {
	return RawMetrics{}, nil
}

func TestRegistryDetect(t *testing.T) {
	tests := []struct {
		name     string
		exts     []Extractor
		wantLang string
		wantErr  error
	}{
		{name: "empty registry", exts: nil, wantErr: ErrNoExtractor},
		{
			name:    "zero matching",
			exts:    []Extractor{fakeExtractor{lang: "go"}, fakeExtractor{lang: "ts"}},
			wantErr: ErrNoExtractor,
		},
		{
			name:     "one matching",
			exts:     []Extractor{fakeExtractor{lang: "go"}, fakeExtractor{lang: "ts", match: true}},
			wantLang: "ts",
		},
		{
			name:    "two matching",
			exts:    []Extractor{fakeExtractor{lang: "go", match: true}, fakeExtractor{lang: "ts", match: true}},
			wantErr: ErrAmbiguousLanguage,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRegistry(tt.exts...)
			if err != nil {
				t.Fatalf("NewRegistry: %v", err)
			}
			got, err := r.Detect("/module")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Detect() error = %v, want %v", err, tt.wantErr)
				}
				if got != nil {
					t.Errorf("Detect() extractor = %v, want nil", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if got.Language() != tt.wantLang {
				t.Errorf("Detect() language = %q, want %q", got.Language(), tt.wantLang)
			}
		})
	}
}

func TestRegistryAmbiguousNamesCandidates(t *testing.T) {
	r, err := NewRegistry(fakeExtractor{lang: "go", match: true}, fakeExtractor{lang: "ts", match: true})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_, err = r.Detect("/module")
	if err == nil || !strings.Contains(err.Error(), "go, ts") {
		t.Errorf("Detect() error = %v, want candidates listed", err)
	}
}

func TestRegistryLookupAndLanguages(t *testing.T) {
	r, err := NewRegistry(fakeExtractor{lang: "go"}, fakeExtractor{lang: "ts"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if got, want := r.Languages(), []string{"go", "ts"}; !slices.Equal(got, want) {
		t.Errorf("Languages() = %v, want %v", got, want)
	}
	if e, ok := r.Lookup("ts"); !ok || e.Language() != "ts" {
		t.Errorf("Lookup(ts) = %v, %v", e, ok)
	}
	if _, ok := r.Lookup("rust"); ok {
		t.Error("Lookup(rust) reported ok")
	}
}

func TestNewRegistryRejectsDuplicateLanguage(t *testing.T) {
	_, err := NewRegistry(fakeExtractor{lang: "go"}, fakeExtractor{lang: "go"})
	if !errors.Is(err, ErrDuplicateLanguage) {
		t.Fatalf("NewRegistry() error = %v, want ErrDuplicateLanguage", err)
	}
}

// TestImportBoundary enforces that this package depends on the standard
// library only, and in particular on no language-specific extractor.
func TestImportBoundary(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("locating go tool: %v", err)
	}
	const self = "github.com/rfizzle/astimate/internal/metrics"
	cmd := exec.CommandContext(t.Context(), goBin, "list", "-deps",
		"-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	for _, path := range strings.Fields(string(out)) {
		if path == self {
			continue
		}
		if strings.HasPrefix(path, "github.com/rfizzle/astimate/internal/lang") {
			t.Errorf("internal/metrics depends on language package %s", path)
			continue
		}
		t.Errorf("internal/metrics depends on non-standard-library package %s", path)
	}
}
