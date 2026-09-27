package typescript

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// tsconfigName is the compiler configuration read for path aliases.
const tsconfigName = "tsconfig.json"

// maxExtendsDepth bounds the length of a tsconfig.json extends chain.
const maxExtendsDepth = 32

// tsconfig is the part of a tsconfig.json the extractor reads, after its
// extends chain is applied: the path aliases under compilerOptions.paths
// and the baseUrl they resolve against.
type tsconfig struct {
	// base is the absolute directory alias targets are relative to:
	// baseUrl when set, else the directory of the configuration file that
	// declared paths, else the module root.
	base string
	// baseURL is the absolute compilerOptions.baseUrl, resolved against
	// the configuration file that set it; "" when none did. Bare
	// specifiers matching no alias resolve under it.
	baseURL string
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

// compilerOptions is the effective baseUrl and paths of one configuration
// file with its extends chain applied, as absolute directories.
type compilerOptions struct {
	// baseURL is the absolute baseUrl; "" when unset.
	baseURL string
	// paths is the paths map, nil when unset; pathsDir the directory of the
	// file that set it.
	paths    map[string][]string
	pathsDir string
}

// readTSConfig reads tsconfig.json at root and the configurations it
// extends. A missing file yields no aliases. Files may hold comments and
// trailing commas, as tsc accepts. An extends value, a string or an array
// of them, is a path relative to the extending file or a bare name
// resolved under node_modules; compilerOptions baseUrl and paths are taken
// from the last configuration that sets each, the extending file last, so
// a child's paths replace its parent's whole, as tsc merges them. An
// extended file that cannot be found, a cycle, or a chain deeper than
// maxExtendsDepth ends the chain there without an error: a configuration
// package that is not installed must not stop the analysis.
func readTSConfig(root string) (tsconfig, error) {
	cfg := tsconfig{base: root}
	p := filepath.Join(root, tsconfigName)
	if !isFile(p) {
		if _, err := os.Stat(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return tsconfig{}, fmt.Errorf("reading %s: %w", p, err)
		}
		return cfg, nil
	}
	opts, err := loadCompilerOptions(p, map[string]bool{}, 0)
	if err != nil {
		return tsconfig{}, err
	}
	cfg.baseURL = opts.baseURL
	switch {
	case opts.baseURL != "":
		cfg.base = opts.baseURL
	case opts.paths != nil:
		cfg.base = opts.pathsDir
	}
	for pattern, targets := range opts.paths {
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

// loadCompilerOptions reads the configuration file at the absolute path p
// and applies its extends chain. seen holds the files already on the chain
// and depth its length so far.
func loadCompilerOptions(p string, seen map[string]bool, depth int) (compilerOptions, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return compilerOptions{}, fmt.Errorf("reading %s: %w", p, err)
	}
	var raw struct {
		Extends         json.RawMessage `json:"extends"`
		CompilerOptions struct {
			BaseURL *string              `json:"baseUrl"`
			Paths   *map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJSONC(data), &raw); err != nil {
		return compilerOptions{}, fmt.Errorf("parsing %s: %w", p, err)
	}
	parents, err := extendsList(raw.Extends)
	if err != nil {
		return compilerOptions{}, fmt.Errorf("parsing %s: %w", p, err)
	}
	seen[p] = true
	defer delete(seen, p)
	dir := filepath.Dir(p)
	var opts compilerOptions
	for _, spec := range parents {
		parent := resolveExtends(dir, spec)
		if parent == "" || seen[parent] || depth+1 > maxExtendsDepth {
			continue
		}
		po, err := loadCompilerOptions(parent, seen, depth+1)
		if err != nil {
			return compilerOptions{}, err
		}
		if po.baseURL != "" {
			opts.baseURL = po.baseURL
		}
		if po.paths != nil {
			opts.paths, opts.pathsDir = po.paths, po.pathsDir
		}
	}
	if b := raw.CompilerOptions.BaseURL; b != nil {
		opts.baseURL = filepath.Join(dir, filepath.FromSlash(*b))
	}
	if ps := raw.CompilerOptions.Paths; ps != nil {
		opts.paths, opts.pathsDir = *ps, dir
		if opts.paths == nil {
			opts.paths = map[string][]string{}
		}
	}
	return opts, nil
}

// extendsList decodes an extends value: absent, a string, or an array of
// strings.
func extendsList(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("extends: want a string or an array of strings: %w", err)
	}
	return many, nil
}

// resolveExtends returns the absolute path of the configuration file the
// extends value spec names, from a file in the directory dir, or "" when
// none exists. A relative or absolute spec names a file, with .json
// appended when it has no such file itself. A bare spec is looked up under
// node_modules in dir and each directory above it: the file it names, that
// name with .json appended, or the tsconfig.json of the package directory
// it names.
func resolveExtends(dir, spec string) string {
	if spec == "" {
		return ""
	}
	if isRelative(spec) || filepath.IsAbs(spec) || path.IsAbs(spec) {
		p := filepath.FromSlash(spec)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		return configFile(p, false)
	}
	for d := dir; ; {
		if p := configFile(filepath.Join(d, "node_modules", filepath.FromSlash(spec)), true); p != "" {
			return p
		}
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		d = up
	}
}

// configFile returns p when it is a file, else p.json when that is one,
// else, with pkgDir set, p/tsconfig.json when that is one; "" otherwise.
func configFile(p string, pkgDir bool) string {
	candidates := []string{p}
	if !strings.HasSuffix(p, ".json") {
		candidates = append(candidates, p+".json")
	}
	if pkgDir {
		candidates = append(candidates, filepath.Join(p, tsconfigName))
	}
	for _, c := range candidates {
		if isFile(c) {
			return c
		}
	}
	return ""
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
