package resolve

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Kind is the class of one import specifier.
type Kind int

const (
	// None is an import counted in no class: a relative one that resolves
	// to no file, or one resolving inside the module to a file whose
	// directory is not a package.
	None Kind = iota
	// Internal resolves to a package of the module.
	Internal
	// External names a package outside the module.
	External
	// Stdlib names a Node built-in module.
	Stdlib
)

// Import is the class of an import and the name it is counted under: the
// package identifier for internal imports, the package name for external
// ones, and the built-in's name for stdlib ones.
type Import struct {
	Kind Kind
	Name string
}

// nodeBuiltins lists Node's built-in modules, which a specifier may name
// with or without the node: prefix. TypeScript has no standard library, so
// stdlib_imports counts these.
func nodeBuiltins() map[string]bool {
	names := []string{
		"assert", "async_hooks", "buffer", "child_process", "cluster", "console",
		"constants", "crypto", "dgram", "diagnostics_channel", "dns", "domain",
		"events", "fs", "http", "http2", "https", "inspector", "module", "net",
		"os", "path", "perf_hooks", "process", "punycode", "querystring",
		"readline", "repl", "stream", "string_decoder", "sys", "timers", "tls",
		"trace_events", "tty", "url", "util", "v8", "vm", "wasi",
		"worker_threads", "zlib",
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// Resolver classifies the import specifiers of one module. It stats the
// file system to find the file an import resolves to and caches the
// answers for the extraction; it is not safe for concurrent use.
type Resolver struct {
	root     string
	pkgs     map[string]struct{}
	cfg      Config
	builtins map[string]bool
	// stat caches what exists at a path; module the file an import of a
	// path resolves to in a pass, "" for none; manifests the entries of
	// the package.json in a directory.
	stat      map[string]statKind
	module    map[moduleKey]string
	manifests map[string]manifest
}

// pass is one of tsc's two resolution passes: every lookup is tried for
// TypeScript files first, and only when that finds nothing, and allowJs is
// set, again for JavaScript files.
type pass int

const (
	// passTS resolves to TypeScript source and declaration files, and to
	// .json files under resolveJsonModule.
	passTS pass = iota
	// passJS resolves to .js, .jsx, .mjs and .cjs files.
	passJS
)

// moduleKey is a path resolved in one pass.
type moduleKey struct {
	path string
	pass pass
}

// manifest is what a package.json names for resolution: its typings and
// types targets, which only the TypeScript pass reads, and its main
// target, which both do; each an absolute path, "" when unset.
type manifest struct {
	typings, types, main string
}

// statKind is what exists at a path.
type statKind int

const (
	statNone statKind = iota
	statFile
	statDir
)

// New returns a resolver for the module at the absolute path root with the
// package identifiers pkgs and the compiler options of cfg, which ReadConfig
// returns.
func New(root string, pkgs map[string]struct{}, cfg Config) *Resolver {
	return &Resolver{
		root: root, pkgs: pkgs, cfg: cfg, builtins: nodeBuiltins(),
		stat: map[string]statKind{}, module: map[moduleKey]string{}, manifests: map[string]manifest{},
	}
}

// Classify returns the class of spec imported from a file in the slash
// directory dir, relative to the module root. A specifier resolves to a
// file as tsc resolves it under moduleResolution bundler (resolveModule):
//
//   - a relative specifier (".", "..", or starting with "./" or "../") is
//     resolved against dir; one that resolves to no file counts nowhere;
//   - a specifier matching a tsconfig.json paths alias resolves to the
//     first of its targets that resolves to a file; when none does, it is
//     classified by the rules below as if no alias matched;
//   - with a baseUrl, a non-relative specifier without the node: prefix
//     that resolves to a file under baseUrl resolves there, as tsc tries
//     baseUrl before node_modules; one that does not falls through. As
//     node_modules is not modelled, this holds too for a name resolving
//     under allowJs only to a JavaScript file there, where tsc would take
//     an installed typed package of that name instead;
//   - a specifier with the node: prefix, or whose first segment is a Node
//     built-in, is stdlib under the built-in's name;
//   - any other specifier is bare and external under its package name, the
//     first segment or, for a scoped package, the first two.
//
// As in tsc, the relative, alias and baseUrl lookups are all tried in the
// TypeScript pass before any is tried in the JavaScript pass, which runs
// only under allowJs; so a directory's index.ts wins over a sibling .js
// file of the same name.
//
// A resolved file inside the module is internal to the package holding
// it, and counted nowhere when its directory is in no package; a resolved
// file outside the module is external under the specifier itself.
func (r *Resolver) Classify(dir, spec string) Import {
	f := r.resolveLocal(dir, spec, passTS)
	if f == "" && r.cfg.allowJS {
		f = r.resolveLocal(dir, spec, passJS)
	}
	if f != "" {
		return r.landed(spec, f)
	}
	if isRelative(spec) {
		return Import{Kind: None}
	}
	if name, ok := strings.CutPrefix(spec, "node:"); ok {
		return Import{Kind: Stdlib, Name: firstSegment(name)}
	}
	if first := firstSegment(spec); r.builtins[first] {
		return Import{Kind: Stdlib, Name: first}
	}
	return Import{Kind: External, Name: packageName(spec)}
}

// resolveLocal returns the file spec, imported from a file in the slash
// directory dir, resolves to in pass ps on the file system: relative to
// dir, else through the first paths alias target that resolves, else under
// baseUrl; "" when none does.
func (r *Resolver) resolveLocal(dir, spec string, ps pass) string {
	if isRelative(spec) {
		p := filepath.Join(r.root, filepath.FromSlash(path.Join(dir, spec)))
		if last := path.Base(spec); strings.HasSuffix(spec, "/") || last == "." || last == ".." {
			// tsc resolves ".", ".." and a trailing slash as a directory
			// only.
			return r.resolveDir(p, ps)
		}
		return r.resolveModule(p, ps)
	}
	for _, t := range r.cfg.match(spec) {
		p := filepath.FromSlash(t)
		if !filepath.IsAbs(p) {
			p = filepath.Join(r.cfg.base, p)
		}
		if f := r.resolveModule(p, ps); f != "" {
			return f
		}
	}
	if r.cfg.baseURL != "" && !strings.HasPrefix(spec, "node:") {
		return r.resolveModule(filepath.Join(r.cfg.baseURL, filepath.FromSlash(spec)), ps)
	}
	return ""
}

// landed classifies an import of spec that resolved to the absolute file
// f: internal under the package holding f, nowhere when f's directory is
// in no package, and external under spec when f is outside the module.
func (r *Resolver) landed(spec, f string) Import {
	rel, err := filepath.Rel(r.root, f)
	if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
		return Import{Kind: External, Name: spec}
	}
	id := packageDir(path.Dir(filepath.ToSlash(rel)))
	if _, ok := r.pkgs[id]; !ok {
		return Import{Kind: None}
	}
	return Import{Kind: Internal, Name: id}
}

// resolveModule returns the file an import of the absolute path p
// resolves to in pass ps, or "" when there is none. As tsc does, it tries
// p as a file (resolveFile) before p as a directory (resolveDir). A file
// with no extension of the pass, and a missing path, resolve to nothing.
func (r *Resolver) resolveModule(p string, ps pass) string {
	k := moduleKey{p, ps}
	if f, ok := r.module[k]; ok {
		return f
	}
	f := r.resolveFile(p, ps)
	if f == "" {
		f = r.resolveDir(p, ps)
	}
	r.module[k] = f
	return f
}

// resolveDir returns the file an import of the absolute directory p
// resolves to in pass ps, or "" for none: the first of its package.json
// entries for the pass (typings, types and main in the TypeScript pass,
// main alone in the JavaScript one) whose target resolves as a file or,
// being a directory, through that directory's index file; failing those,
// its own index file (resolveIndex). A directory with neither resolves to
// nothing.
func (r *Resolver) resolveDir(p string, ps pass) string {
	if r.kindOf(p) != statDir {
		return ""
	}
	m := r.manifestEntries(p)
	entries := []string{m.typings, m.types, m.main}
	if ps == passJS {
		entries = entries[2:]
	}
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		f := r.resolveFile(entry, ps)
		if f == "" && r.kindOf(entry) == statDir {
			f = r.resolveIndex(entry, ps)
		}
		if f != "" {
			return f
		}
	}
	return r.resolveIndex(p, ps)
}

// resolveFile returns the file an import of the absolute path p names in
// pass ps, or "" for none. In the TypeScript pass: p itself when it ends
// in .json under resolveJsonModule; else the TypeScript files tsc tries
// for p's extension (sourceCandidates), then, as tsc retries by appending
// an extension, p with .ts, .tsx or .d.ts appended, so an extensionless
// name gets only the appended forms and "a.js" may resolve to "a.js.ts".
// In the JavaScript pass: nothing for TypeScript names or those same JSON
// names; the JavaScript files of a JavaScript extension
// (scriptCandidates); else p with .js or .jsx appended.
func (r *Resolver) resolveFile(p string, ps pass) string {
	var candidates []string
	isJSON := r.cfg.resolveJSON && strings.HasSuffix(p, ".json")
	switch {
	case ps == passJS && (isJSON || IsTypeScriptName(p)):
	case ps == passJS && hasJSExtension(p):
		candidates = scriptCandidates(p)
	case ps == passJS:
		candidates = []string{p + ".js", p + ".jsx"}
	case isJSON:
		candidates = []string{p}
	default:
		candidates = append(sourceCandidates(p), p+".ts", p+".tsx", p+".d.ts")
	}
	for _, c := range candidates {
		if r.kindOf(c) == statFile {
			return c
		}
	}
	return ""
}

// resolveIndex returns the first index file in the absolute directory dir
// for pass ps, or "" for none: in the TypeScript pass index.ts, index.tsx,
// then index.d.ts; in the JavaScript pass index.js, then index.jsx. As in
// tsc, which resolves a directory as the extensionless name "index", no
// .mts or .cts form is tried.
func (r *Resolver) resolveIndex(dir string, ps pass) string {
	names := []string{"index.ts", "index.tsx", "index.d.ts"}
	if ps == passJS {
		names = []string{"index.js", "index.jsx"}
	}
	for _, name := range names {
		if c := filepath.Join(dir, name); r.kindOf(c) == statFile {
			return c
		}
	}
	return ""
}

// manifestEntries returns the absolute paths the package.json in the
// absolute directory dir names in its typings, types and main fields; the
// zero manifest when dir has no readable, valid package.json, since tsc
// then goes on to the index file.
// Each package.json is read at most once per resolver.
func (r *Resolver) manifestEntries(dir string) manifest {
	if m, ok := r.manifests[dir]; ok {
		return m
	}
	var m manifest
	if p := filepath.Join(dir, ManifestName); r.kindOf(p) == statFile {
		m = readManifestEntries(dir, p)
	}
	r.manifests[dir] = m
	return m
}

// readManifestEntries reads the package.json at p in the directory dir for
// manifestEntries.
func readManifestEntries(dir, p string) manifest {
	data, err := os.ReadFile(p)
	if err != nil {
		return manifest{}
	}
	var raw struct {
		Typings any `json:"typings"`
		Types   any `json:"types"`
		Main    any `json:"main"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return manifest{}
	}
	entry := func(v any) string {
		if s, ok := v.(string); ok && s != "" {
			return filepath.Join(dir, filepath.FromSlash(s))
		}
		return ""
	}
	return manifest{typings: entry(raw.Typings), types: entry(raw.Types), main: entry(raw.Main)}
}

// kindOf reports what exists at the absolute path p, caching the answer.
func (r *Resolver) kindOf(p string) statKind {
	if k, ok := r.stat[p]; ok {
		return k
	}
	k := statNone
	if fi, err := os.Stat(p); err == nil {
		switch {
		case fi.Mode().IsRegular():
			k = statFile
		case fi.IsDir():
			k = statDir
		}
	}
	r.stat[p] = k
	return k
}

// hasJSExtension reports whether p ends in .js, .jsx, .mjs or .cjs.
func hasJSExtension(p string) bool {
	return hasSuffix(p, ".js", ".jsx", ".mjs", ".cjs")
}

// sourceCandidates returns the TypeScript files tsc tries for an import of
// p written with a TypeScript or JavaScript extension, in its order, by
// replacing that extension (tsc's tryAddingExtensions): .ts, .tsx, .d.ts
// for .ts, .d.ts and .js; .tsx, .ts, .d.ts for .tsx and .jsx; .mts, .d.mts
// for .mts, .d.mts and .mjs; .cts, .d.cts for .cts, .d.cts and .cjs. It
// returns no candidates for any other p.
func sourceCandidates(p string) []string {
	var exts []string
	stem := p
	// The declaration forms come before the extensions they end in, so the
	// stem of a.d.ts is a, not a.d.
	for _, ext := range []string{".d.ts", ".d.mts", ".d.cts", ".ts", ".js", ".tsx", ".jsx", ".mts", ".mjs", ".cts", ".cjs"} {
		if s, ok := strings.CutSuffix(p, ext); ok {
			stem, exts = s, replacements(ext)
			break
		}
	}
	// Room for the three forms resolveFile appends.
	out := make([]string, 0, len(exts)+3)
	for _, ext := range exts {
		out = append(out, stem+ext)
	}
	return out
}

// replacements returns the extensions sourceCandidates tries, in order, for
// an import written with the extension ext.
func replacements(ext string) []string {
	switch ext {
	case ".d.ts", ".ts", ".js":
		return []string{".ts", ".tsx", ".d.ts"}
	case ".tsx", ".jsx":
		return []string{".tsx", ".ts", ".d.ts"}
	case ".d.mts", ".mts", ".mjs":
		return []string{".mts", ".d.mts"}
	default:
		return []string{".cts", ".d.cts"}
	}
}

// scriptCandidates returns the JavaScript files tsc tries under allowJs
// for an import of p written with a JavaScript extension, in its order:
// .js then .jsx for .js, .jsx then .js for .jsx, and the file itself for
// .mjs and .cjs; nil for any other p.
func scriptCandidates(p string) []string {
	if stem, ok := strings.CutSuffix(p, ".jsx"); ok {
		return []string{p, stem + ".js"}
	}
	if stem, ok := strings.CutSuffix(p, ".js"); ok {
		return []string{p, stem + ".jsx"}
	}
	if hasJSExtension(p) {
		return []string{p}
	}
	return nil
}

// isRelative reports whether spec is a relative module specifier.
func isRelative(spec string) bool {
	return spec == "." || spec == ".." || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../")
}

// firstSegment returns spec up to its first slash.
func firstSegment(spec string) string {
	first, _, _ := strings.Cut(spec, "/")
	return first
}

// packageName returns the npm package a bare specifier names: its first
// segment, or its first two for a scoped package (@scope/name).
func packageName(spec string) string {
	segs := strings.SplitN(spec, "/", 3)
	if strings.HasPrefix(spec, "@") && len(segs) >= 2 {
		return segs[0] + "/" + segs[1]
	}
	return segs[0]
}
