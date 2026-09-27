package typescript

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// tsconfigName is the compiler configuration read for path aliases.
const tsconfigName = "tsconfig.json"

// tsconfig is the part of a tsconfig.json the extractor reads: the path
// aliases under compilerOptions.paths and the baseUrl they resolve against.
type tsconfig struct {
	// base is the absolute directory alias targets are relative to:
	// baseUrl resolved against the module root, or the root without one.
	base string
	// aliases holds each paths entry, longest prefix first.
	aliases []alias
}

// alias is one compilerOptions.paths entry. pattern holds at most one *.
type alias struct {
	prefix, suffix string
	// wildcard reports whether the pattern holds a *.
	wildcard bool
	targets  []string
}

// readTSConfig reads tsconfig.json at root. A missing file yields no
// aliases. The file may hold comments and trailing commas, as tsc accepts.
// "extends" is not followed.
func readTSConfig(root string) (tsconfig, error) {
	cfg := tsconfig{base: root}
	p := filepath.Join(root, tsconfigName)
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return tsconfig{}, fmt.Errorf("reading %s: %w", p, err)
	}
	var raw struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJSONC(data), &raw); err != nil {
		return tsconfig{}, fmt.Errorf("parsing %s: %w", p, err)
	}
	if b := raw.CompilerOptions.BaseURL; b != "" {
		cfg.base = filepath.Join(root, filepath.FromSlash(b))
	}
	for pattern, targets := range raw.CompilerOptions.Paths {
		if len(targets) == 0 {
			continue
		}
		a := alias{prefix: pattern, targets: targets}
		if i := strings.IndexByte(pattern, '*'); i >= 0 {
			a.prefix, a.suffix, a.wildcard = pattern[:i], pattern[i+1:], true
		}
		cfg.aliases = append(cfg.aliases, a)
	}
	// Exact patterns first, then longest prefix, as tsc matches; the
	// pattern text breaks ties so the order does not depend on the map.
	slices.SortFunc(cfg.aliases, func(a, b alias) int {
		if a.wildcard != b.wildcard {
			if !a.wildcard {
				return -1
			}
			return 1
		}
		if len(a.prefix) != len(b.prefix) {
			return len(b.prefix) - len(a.prefix)
		}
		return strings.Compare(a.prefix+"*"+a.suffix, b.prefix+"*"+b.suffix)
	})
	return cfg, nil
}

// match returns the targets of the first alias spec matches, with the
// wildcard substituted, relative to c.base; nil when none matches.
func (c *tsconfig) match(spec string) []string {
	for _, a := range c.aliases {
		if !a.wildcard {
			if spec == a.prefix {
				return a.targets
			}
			continue
		}
		if len(spec) < len(a.prefix)+len(a.suffix) ||
			!strings.HasPrefix(spec, a.prefix) || !strings.HasSuffix(spec, a.suffix) {
			continue
		}
		star := spec[len(a.prefix) : len(spec)-len(a.suffix)]
		out := make([]string, 0, len(a.targets))
		for _, t := range a.targets {
			out = append(out, strings.Replace(t, "*", star, 1))
		}
		return out
	}
	return nil
}

// stripJSONC returns data with // and /* */ comments outside strings
// replaced by spaces, and commas directly before a closing ] or } removed,
// so encoding/json accepts a tsconfig.json.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString, escaped := false, false
	for i := 0; i < len(data); i++ {
		b := data[i]
		if inString {
			out = append(out, b)
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				inString = false
			}
			continue
		}
		switch {
		case b == '"':
			inString = true
			out = append(out, b)
		case b == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
		case b == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && (data[i] != '*' || data[i+1] != '/') {
				i++
			}
			i++
			out = append(out, ' ')
		case b == ']' || b == '}':
			// Drop a trailing comma, skipping the white space after it.
			j := len(out) - 1
			for j >= 0 && isSpace(out[j]) || j >= 0 && out[j] == '\n' {
				j--
			}
			if j >= 0 && out[j] == ',' {
				out = append(out[:j], out[j+1:]...)
			}
			out = append(out, b)
		default:
			out = append(out, b)
		}
	}
	return out
}
