package metrics

import (
	"errors"
	"slices"
	"testing"
)

// skippingExtractor is a fakeExtractor that lists packages that fail to
// load (Skipper) and packages no file of which is built (Excluder).
type skippingExtractor struct {
	fakeExtractor
	skipped, excluded []SkippedPackage
	err               error
}

func (s skippingExtractor) Skipped(string) ([]SkippedPackage, error)  { return s.skipped, s.err }
func (s skippingExtractor) Excluded(string) ([]SkippedPackage, error) { return s.excluded, nil }

// TestNotAnalysed checks that NotAnalysed merges what Skipper and Excluder
// list, sorted by identifier, lists nothing for an extractor that
// implements neither, and fails when a list does.
func TestNotAnalysed(t *testing.T) {
	broken := SkippedPackage{Package: "m/b", Reason: "missing module", Error: "no required module"}
	gen := SkippedPackage{Package: "m/gen", Reason: "build constraints exclude all Go files"}
	asm := SkippedPackage{Package: "m/a", Reason: "build constraints exclude all Go files"}
	got, err := NotAnalysed(skippingExtractor{skipped: []SkippedPackage{broken}, excluded: []SkippedPackage{asm, gen}}, "/root")
	if want := []SkippedPackage{asm, broken, gen}; err != nil || !slices.Equal(got, want) {
		t.Errorf("NotAnalysed = %+v, %v; want %+v", got, err, want)
	}
	if got, err := NotAnalysed(fakeExtractor{}, "/root"); err != nil || len(got) != 0 {
		t.Errorf("NotAnalysed of a plain extractor = %+v, %v; want none", got, err)
	}
	failure := errors.New("load failed")
	if _, err := NotAnalysed(skippingExtractor{err: failure}, "/root"); !errors.Is(err, failure) {
		t.Errorf("NotAnalysed error = %v, want %v", err, failure)
	}
}
