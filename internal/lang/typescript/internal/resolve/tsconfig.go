package resolve

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

// Config is the part of a tsconfig.json the extractor reads, after its
// extends chain is applied: the path aliases under compilerOptions.paths
// and the baseUrl they resolve against, whether JSON modules resolve, and
// whether JavaScript files do.
type Config struct {
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
	// resolveJSON is compilerOptions.resolveJsonModule: a specifier
	// ending in .json then resolves to that file.
	resolveJSON bool
	// allowJS is compilerOptions.allowJs, which checkJs implies when
	// allowJs is unset, as in tsc: JavaScript files are then resolution
	// targets after the TypeScript ones.
	allowJS bool
}

// alias is one compilerOptions.paths entry. pattern holds at most one *.
type alias struct {
	prefix, suffix string
	// wildcard reports whether the pattern holds a *.
	wildcard bool
	targets  []string
}

// compilerOptions is the effective baseUrl, paths, resolveJsonModule,
// allowJs and checkJs of one configuration file with its extends chain applied, the
// directories absolute.
type compilerOptions struct {
	// baseURL is the absolute baseUrl; "" when unset.
	baseURL string
	// paths is the paths map, nil when unset; pathsDir the directory of the
	// file that set it.
	paths    map[string][]string
	pathsDir string
	// resolveJSON is resolveJsonModule, nil when unset.
	resolveJSON *bool
	// allowJS is allowJs and checkJS checkJs, each nil when unset.
	allowJS, checkJS *bool
}

// ReadConfig reads tsconfig.json at root and the configurations it
// extends. A missing file yields no aliases. Files may hold comments and
// trailing commas, as tsc accepts. An extends value, a string or an array
// of them, is a path relative to the extending file or a bare name
// resolved under node_modules; compilerOptions baseUrl, paths,
// resolveJsonModule, allowJs and checkJs are taken from the last
// configuration that sets each, the extending file last, so a child's
// paths replace its parent's whole, as tsc merges them. As in
// tsc 5.5, a baseUrl, paths target or extends value starting with
// ${configDir} has it replaced by root, the directory of the root
// configuration, in every file of the chain. An extended file that cannot
// be found, a cycle, or a chain deeper than maxExtendsDepth ends the chain
// there without an error: a configuration package that is not installed
// must not stop the analysis.
func ReadConfig(root string) (Config, error) {
	cfg := Config{base: root}
	p := filepath.Join(root, tsconfigName)
	if !isFile(p) {
		if _, err := os.Stat(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Config{}, fmt.Errorf("reading %s: %w", p, err)
		}
		return cfg, nil
	}
	opts, err := loadCompilerOptions(p, root, map[string]bool{}, 0)
	if err != nil {
		return Config{}, err
	}
	cfg.baseURL = opts.baseURL
	cfg.resolveJSON = opts.resolveJSON != nil && *opts.resolveJSON
	switch {
	case opts.allowJS != nil:
		cfg.allowJS = *opts.allowJS
	case opts.checkJS != nil:
		cfg.allowJS = *opts.checkJS
	}
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
// and applies its extends chain. configDir is the directory of the root
// configuration, which ${configDir} stands for; seen holds the files
// already on the chain and depth its length so far.
func loadCompilerOptions(p, configDir string, seen map[string]bool, depth int) (compilerOptions, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return compilerOptions{}, fmt.Errorf("reading %s: %w", p, err)
	}
	var raw struct {
		Extends         json.RawMessage `json:"extends"`
		CompilerOptions struct {
			BaseURL *string              `json:"baseUrl"`
			Paths   *map[string][]string `json:"paths"`
			// ResolveJSON is resolveJsonModule.
			ResolveJSON *bool `json:"resolveJsonModule"`
			AllowJS     *bool `json:"allowJs"`
			CheckJS     *bool `json:"checkJs"`
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
		if abs, ok := expandConfigDir(spec, configDir); ok {
			spec = abs
		}
		parent := resolveExtends(dir, spec)
		if parent == "" || seen[parent] || depth+1 > maxExtendsDepth {
			continue
		}
		po, err := loadCompilerOptions(parent, configDir, seen, depth+1)
		if err != nil {
			return compilerOptions{}, err
		}
		if po.baseURL != "" {
			opts.baseURL = po.baseURL
		}
		if po.paths != nil {
			opts.paths, opts.pathsDir = po.paths, po.pathsDir
		}
		overrideBool(&opts.resolveJSON, po.resolveJSON)
		overrideBool(&opts.allowJS, po.allowJS)
		overrideBool(&opts.checkJS, po.checkJS)
	}
	if b := raw.CompilerOptions.BaseURL; b != nil {
		if abs, ok := expandConfigDir(*b, configDir); ok {
			opts.baseURL = abs
		} else {
			opts.baseURL = filepath.Join(dir, filepath.FromSlash(*b))
		}
	}
	overrideBool(&opts.resolveJSON, raw.CompilerOptions.ResolveJSON)
	overrideBool(&opts.allowJS, raw.CompilerOptions.AllowJS)
	overrideBool(&opts.checkJS, raw.CompilerOptions.CheckJS)
	if ps := raw.CompilerOptions.Paths; ps != nil {
		opts.paths, opts.pathsDir = *ps, dir
		if opts.paths == nil {
			opts.paths = map[string][]string{}
		}
		for _, targets := range opts.paths {
			for i, t := range targets {
				if abs, ok := expandConfigDir(t, configDir); ok {
					targets[i] = filepath.ToSlash(abs)
				}
			}
		}
	}
	return opts, nil
}

// overrideBool sets *dst to v when v is set, as a later configuration of
// an extends chain overrides an earlier one.
func overrideBool(dst **bool, v *bool) {
	if v != nil {
		*dst = v
	}
}

// configDirVar is the template tsc 5.5 replaces, at the start of a path
// option, with the directory of the root configuration.
const configDirVar = "${configDir}"

// expandConfigDir returns v with a leading ${configDir} replaced by the
// absolute directory configDir, and true; v and false when v does not
// start with it.
func expandConfigDir(v, configDir string) (string, bool) {
	rest, ok := strings.CutPrefix(v, configDirVar)
	if !ok {
		return v, false
	}
	return filepath.Join(configDir, filepath.FromSlash(rest)), true
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
// node_modules in dir and each directory above it (packageConfig).
func resolveExtends(dir, spec string) string {
	if spec == "" {
		return ""
	}
	if isRelative(spec) || filepath.IsAbs(spec) || path.IsAbs(spec) {
		p := filepath.FromSlash(spec)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		return configFile(p)
	}
	for d := dir; ; {
		if p := packageConfig(filepath.Join(d, "node_modules"), spec); p != "" {
			return p
		}
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		d = up
	}
}

// packageConfig returns the configuration file the bare extends value spec
// names in the node_modules directory nm, or "" for none: the file it
// names, or that name with .json appended; for a spec naming a whole
// package, then the file its package.json names (manifestConfig); and last
// the tsconfig.json of the directory it names.
func packageConfig(nm, spec string) string {
	p := filepath.Join(nm, filepath.FromSlash(spec))
	if f := configFile(p); f != "" {
		return f
	}
	if spec == packageName(spec) {
		if f := manifestConfig(p); f != "" {
			return f
		}
	}
	return configFile(filepath.Join(p, tsconfigName))
}

// manifestConfig returns the configuration file the package.json in the
// package directory dir names, or "" for none: the target of its exports
// field when that is a string or an object whose "." entry is one, else
// the file its tsconfig field names. An exports value of any other shape
// (conditions, subpaths only) and a target that is no file are passed
// over.
func manifestConfig(dir string) string {
	p := filepath.Join(dir, ManifestName)
	if !isFile(p) {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var m struct {
		Exports  json.RawMessage `json:"exports"`
		TSConfig any             `json:"tsconfig"`
	}
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	var targets []string
	var s string
	var entries map[string]json.RawMessage
	switch {
	case json.Unmarshal(m.Exports, &s) == nil:
		targets = append(targets, s)
	case json.Unmarshal(m.Exports, &entries) == nil && json.Unmarshal(entries["."], &s) == nil:
		targets = append(targets, s)
	}
	if s, ok := m.TSConfig.(string); ok {
		targets = append(targets, s)
	}
	for _, t := range targets {
		if t == "" {
			continue
		}
		if f := configFile(filepath.Join(dir, filepath.FromSlash(t))); f != "" {
			return f
		}
	}
	return ""
}

// configFile returns p when it is a file, else p.json when that is one;
// "" otherwise.
func configFile(p string) string {
	candidates := []string{p}
	if !strings.HasSuffix(p, ".json") {
		candidates = append(candidates, p+".json")
	}
	for _, c := range candidates {
		if isFile(c) {
			return c
		}
	}
	return ""
}

// match returns the targets of the first alias spec matches, with the
// wildcard substituted, relative to c.base unless ${configDir} made them
// absolute; nil when none matches.
func (c *Config) match(spec string) []string {
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

// isSpace reports whether b is ASCII white space other than a newline.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\v' || b == '\f'
}
