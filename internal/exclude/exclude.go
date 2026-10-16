// Package exclude matches source files against the configuration's
// exclude list (SPEC.md 6.5 and section 9), the one matcher both
// extractors apply at their file walk. It imports the standard library
// only.
package exclude

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// ReasonPrefix starts the reason a package is not analysed with when the
// list matches every one of its non-test source files (Reason).
const ReasonPrefix = "excluded by config"

// Reason returns the reason an extractor gives a package left out because
// pattern matched its non-test source files, for example
// "excluded by config (internal/gen/**)".
func Reason(pattern string) string {
	return ReasonPrefix + " (" + pattern + ")"
}

// List is the configuration's exclude list: patterns matched against a
// source file's slash-separated path relative to the module root. Within
// a segment *, ? and [...] match as in path.Match; a ** segment matches
// any number of whole segments, none included; and a pattern that
// matches a directory of the path matches every file under it. Build it
// with New. A nil List matches nothing. It is read-only and safe for
// concurrent use.
type List struct {
	patterns []string
	segs     [][]string
}

// New compiles patterns, keeping their order, so Match reports the first
// that matches. A pattern that is empty or absolute, or holds an empty,
// "." or ".." segment, a ** within a longer segment or a malformed
// character class, is an error naming its index and the pattern; every
// bad pattern is reported. No pattern yields a nil List.
func New(patterns []string) (*List, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	l := &List{patterns: make([]string, 0, len(patterns)), segs: make([][]string, 0, len(patterns))}
	var errs []error
	for i, p := range patterns {
		segs, err := compile(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("exclude[%d] %q: %w", i, p, err))
			continue
		}
		l.patterns = append(l.patterns, p)
		l.segs = append(l.segs, segs)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return l, nil
}

// compile splits p into its segments, a trailing slash dropped, and
// checks each.
func compile(p string) ([]string, error) {
	switch {
	case strings.TrimSpace(p) == "":
		return nil, errors.New("empty pattern")
	case strings.HasPrefix(p, "/"):
		return nil, errors.New("pattern must be relative to the module root")
	}
	segs := strings.Split(strings.TrimSuffix(p, "/"), "/")
	for _, s := range segs {
		switch {
		case s == "":
			return nil, errors.New("empty path segment")
		case s == "." || s == "..":
			return nil, fmt.Errorf("%q segment not allowed", s)
		case s != "**" && strings.Contains(s, "**"):
			return nil, errors.New("** must be a whole path segment")
		}
		if _, err := path.Match(s, ""); err != nil {
			return nil, fmt.Errorf("malformed segment %q: %w", s, err)
		}
	}
	return segs, nil
}

// Patterns returns the patterns in configuration order; nil for a nil
// List. Callers must not modify it.
func (l *List) Patterns() []string {
	if l == nil {
		return nil
	}
	return l.patterns
}

// Match returns the index and text of the first pattern of l that matches
// the file at rel, a slash path relative to the module root, and whether
// one does. A nil l, and a path outside the root, match nothing.
func (l *List) Match(rel string) (index int, pattern string, ok bool) {
	if l == nil {
		return -1, "", false
	}
	rel = path.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
		return -1, "", false
	}
	segs := strings.Split(rel, "/")
	for i, pat := range l.segs {
		if matchSegments(pat, segs) {
			return i, l.patterns[i], true
		}
	}
	return -1, "", false
}

// Matches reports whether some pattern of l matches the file at rel.
func (l *List) Matches(rel string) bool {
	_, _, ok := l.Match(rel)
	return ok
}

// matchSegments reports whether pat matches segs or a leading run of them,
// a directory the path lies under.
func matchSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return true
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegments(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, _ := path.Match(pat[0], segs[0])
	return ok && matchSegments(pat[1:], segs[1:])
}
