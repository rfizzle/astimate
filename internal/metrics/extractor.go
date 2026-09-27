package metrics

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Extractor computes RawMetrics for the packages of one language. Language
// specific code lives only in implementations under internal/lang.
type Extractor interface {
	// Language returns the identifier the extractor is registered under, for
	// example "go".
	Language() string
	// Detect reports whether the module rooted at root is written in this
	// extractor's language.
	Detect(root string) bool
	// Packages lists the package identifiers under root that Extract accepts.
	Packages(root string) ([]string, error)
	// Extract computes the metrics of pkg within the module described by mod.
	// Implementations may store module-wide state in mod.Cache so it is built
	// once per invocation rather than once per package.
	Extract(ctx context.Context, mod *ModuleContext, pkg string) (RawMetrics, error)
}

// ModuleContext describes the module being analyzed and carries state shared
// across Extract calls for that module.
type ModuleContext struct {
	// Root is the filesystem path of the module root.
	Root string
	// ModulePath is the module's import path, for example
	// "github.com/rfizzle/astimate".
	ModulePath string
	// Cache is an opaque slot owned by the extractor, used to hold module-wide
	// data such as loaded packages or the reverse import graph. Only the
	// extractor that set it may interpret it, and that extractor is
	// responsible for synchronizing access if it calls Extract concurrently.
	Cache any
}

// ErrNoExtractor is returned by Registry.Detect when no registered extractor
// matches the module root.
var ErrNoExtractor = errors.New("no extractor matches")

// ErrAmbiguousLanguage is returned by Registry.Detect when more than one
// registered extractor matches the module root.
var ErrAmbiguousLanguage = errors.New("more than one extractor matches")

// ErrDuplicateLanguage is returned by NewRegistry when two extractors report
// the same language identifier.
var ErrDuplicateLanguage = errors.New("duplicate extractor language")

// Registry maps language identifiers to extractors. Construct it with
// NewRegistry; it is read-only afterwards and safe for concurrent use.
type Registry struct {
	ordered []Extractor
	byLang  map[string]Extractor
}

// NewRegistry returns a registry holding extractors in the given order. It
// fails when two extractors share a language identifier.
func NewRegistry(extractors ...Extractor) (*Registry, error) {
	r := &Registry{
		ordered: make([]Extractor, 0, len(extractors)),
		byLang:  make(map[string]Extractor, len(extractors)),
	}
	for _, e := range extractors {
		lang := e.Language()
		if _, ok := r.byLang[lang]; ok {
			return nil, fmt.Errorf("registering %q: %w", lang, ErrDuplicateLanguage)
		}
		r.byLang[lang] = e
		r.ordered = append(r.ordered, e)
	}
	return r, nil
}

// Lookup returns the extractor registered for language, if any.
func (r *Registry) Lookup(language string) (Extractor, bool) {
	e, ok := r.byLang[language]
	return e, ok
}

// Languages returns the registered language identifiers in registration
// order.
func (r *Registry) Languages() []string {
	langs := make([]string, 0, len(r.ordered))
	for _, e := range r.ordered {
		langs = append(langs, e.Language())
	}
	return langs
}

// Detect selects the single extractor whose Detect reports true for root. It
// returns an error wrapping ErrNoExtractor when none match and
// ErrAmbiguousLanguage, naming the candidates, when several match.
func (r *Registry) Detect(root string) (Extractor, error) {
	var matches []Extractor
	for _, e := range r.ordered {
		if e.Detect(root) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("detecting language of %s: %w", root, ErrNoExtractor)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, e := range matches {
			names = append(names, e.Language())
		}
		return nil, fmt.Errorf("detecting language of %s: %w: %s",
			root, ErrAmbiguousLanguage, strings.Join(names, ", "))
	}
}
