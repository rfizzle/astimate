package typescript

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// importKind is the class of one import specifier.
type importKind int

const (
	// importNone is an import counted in no class: a relative one that
	// resolves to no file, or one resolving inside the module to a file
	// whose directory is not a package.
	importNone importKind = iota
	// importInternal resolves to a package of the module.
	importInternal
	// importExternal names a package outside the module.
	importExternal
	// importStdlib names a Node built-in module.
	importStdlib
)

// classified is the class of an import and the name it is counted under:
// the package identifier for internal imports, the package name for
// external ones, and the built-in's name for stdlib ones.
type classified struct {
	kind importKind
	name string
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

// resolver classifies the import specifiers of one module. It stats the
// file system to find the file an import resolves to and caches the
// answers for the extraction; it is not safe for concurrent use.
type resolver struct {
	root     string
	pkgs     map[string]*pkg
	cfg      tsconfig
	builtins map[string]bool
	// stat caches what exists at a path; module the file an import of a
	// path resolves to, "" for none; manifests the entries of the
	// package.json in a directory.
	stat      map[string]statKind
	module    map[string]string
	manifests map[string][]string
}

// statKind is what exists at a path.
type statKind int

const (
	statNone statKind = iota
	statFile
	statDir
)

// newResolver returns a resolver for the module at the absolute path root
// with packages pkgs and the path aliases of cfg.
func newResolver(root string, pkgs map[string]*pkg, cfg tsconfig) *resolver {
	return &resolver{
		root: root, pkgs: pkgs, cfg: cfg, builtins: nodeBuiltins(),
		stat: map[string]statKind{}, module: map[string]string{}, manifests: map[string][]string{},
	}
}

// classify returns the class of spec imported from a file in the slash
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
//     baseUrl before node_modules; one that does not falls through;
//   - a specifier with the node: prefix, or whose first segment is a Node
//     built-in, is stdlib under the built-in's name;
//   - any other specifier is bare and external under its package name, the
//     first segment or, for a scoped package, the first two.
//
// A resolved file inside the module is internal to the package holding
// it, and counted nowhere when its directory is in no package; a resolved
// file outside the module is external under the specifier itself.
func (r *resolver) classify(dir, spec string) classified {
	if isRelative(spec) {
		p := filepath.Join(r.root, filepath.FromSlash(path.Join(dir, spec)))
		var f string
		if last := path.Base(spec); strings.HasSuffix(spec, "/") || last == "." || last == ".." {
			// tsc resolves ".", ".." and a trailing slash as a directory
			// only.
			f = r.resolveDir(p)
		} else {
			f = r.resolveModule(p)
		}
		if f == "" {
			return classified{kind: importNone}
		}
		return r.landed(spec, f)
	}
	for _, t := range r.cfg.match(spec) {
		p := filepath.FromSlash(t)
		if !filepath.IsAbs(p) {
			p = filepath.Join(r.cfg.base, p)
		}
		if f := r.resolveModule(p); f != "" {
			return r.landed(spec, f)
		}
	}
	if r.cfg.baseURL != "" && !strings.HasPrefix(spec, "node:") {
		if f := r.resolveModule(filepath.Join(r.cfg.baseURL, filepath.FromSlash(spec))); f != "" {
			return r.landed(spec, f)
		}
	}
	if name, ok := strings.CutPrefix(spec, "node:"); ok {
		return classified{kind: importStdlib, name: firstSegment(name)}
	}
	if first := firstSegment(spec); r.builtins[first] {
		return classified{kind: importStdlib, name: first}
	}
	return classified{kind: importExternal, name: packageName(spec)}
}

// landed classifies an import of spec that resolved to the absolute file
// f: internal under the package holding f, nowhere when f's directory is
// in no package, and external under spec when f is outside the module.
func (r *resolver) landed(spec, f string) classified {
	rel, err := filepath.Rel(r.root, f)
	if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
		return classified{kind: importExternal, name: spec}
	}
	id := packageDir(path.Dir(filepath.ToSlash(rel)))
	if _, ok := r.pkgs[id]; !ok {
		return classified{kind: importNone}
	}
	return classified{kind: importInternal, name: id}
}

// resolveModule returns the file an import of the absolute path p
// resolves to, or "" when there is none. As tsc does, it tries p as a file
// (resolveFile) before p as a directory (resolveDir). A file with no
// TypeScript extension after the JavaScript mapping and a missing path
// resolve to nothing.
func (r *resolver) resolveModule(p string) string {
	if f, ok := r.module[p]; ok {
		return f
	}
	f := r.resolveFile(p)
	if f == "" {
		f = r.resolveDir(p)
	}
	r.module[p] = f
	return f
}

// resolveDir returns the file an import of the absolute directory p
// resolves to, or "" for none: the first of its package.json typings,
// types and main fields whose target resolves as a file or, being a
// directory, through that directory's index file; failing those, its own
// index file (resolveIndex). A directory with neither resolves to nothing.
func (r *resolver) resolveDir(p string) string {
	if r.kindOf(p) != statDir {
		return ""
	}
	for _, entry := range r.manifestEntries(p) {
		f := r.resolveFile(entry)
		if f == "" && r.kindOf(entry) == statDir {
			f = r.resolveIndex(entry)
		}
		if f != "" {
			return f
		}
	}
	return r.resolveIndex(p)
}

// resolveFile returns the file an import of the absolute path p names, or
// "" for none: p itself when it ends in a TypeScript source or declaration
// extension; the TypeScript counterparts of a JavaScript extension
// (sourceCandidates); else p with .ts, .tsx or .d.ts appended.
func (r *resolver) resolveFile(p string) string {
	var candidates []string
	switch {
	case hasTSExtension(p):
		candidates = []string{p}
	case hasJSExtension(p):
		candidates = sourceCandidates(p)
	default:
		candidates = []string{p + ".ts", p + ".tsx", p + ".d.ts"}
	}
	for _, c := range candidates {
		if r.kindOf(c) == statFile {
			return c
		}
	}
	return ""
}

// resolveIndex returns the first index file in the absolute directory dir,
// or "" for none: index.ts, index.tsx and index.d.ts, then the .mts and
// .cts forms.
func (r *resolver) resolveIndex(dir string) string {
	for _, name := range []string{
		"index.ts", "index.tsx", "index.d.ts",
		"index.mts", "index.cts", "index.d.mts", "index.d.cts",
	} {
		if c := filepath.Join(dir, name); r.kindOf(c) == statFile {
			return c
		}
	}
	return ""
}

// manifestEntries returns the absolute paths the package.json in the
// absolute directory dir names in its typings, types and main fields, in
// that order, skipping unset ones; nil when dir has no readable, valid
// package.json, since tsc then goes on to the index file.
// Each package.json is read at most once per resolver.
func (r *resolver) manifestEntries(dir string) []string {
	if entries, ok := r.manifests[dir]; ok {
		return entries
	}
	var entries []string
	if p := filepath.Join(dir, manifestName); r.kindOf(p) == statFile {
		entries = readManifestEntries(dir, p)
	}
	r.manifests[dir] = entries
	return entries
}

// readManifestEntries reads the package.json at p in the directory dir for
// manifestEntries.
func readManifestEntries(dir, p string) []string {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m struct {
		Typings any `json:"typings"`
		Types   any `json:"types"`
		Main    any `json:"main"`
	}
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	out := make([]string, 0, 3)
	for _, v := range []any{m.Typings, m.Types, m.Main} {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, filepath.Join(dir, filepath.FromSlash(s)))
		}
	}
	return out
}

// kindOf reports what exists at the absolute path p, caching the answer.
func (r *resolver) kindOf(p string) statKind {
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

// hasTSExtension reports whether p ends in a TypeScript source or
// declaration extension: .ts, .tsx, .mts or .cts, which the declaration
// forms (.d.ts, .d.mts, .d.cts) end in too.
func hasTSExtension(p string) bool {
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts"} {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

// hasJSExtension reports whether p ends in .js, .jsx, .mjs or .cjs.
func hasJSExtension(p string) bool {
	for _, ext := range []string{".js", ".jsx", ".mjs", ".cjs"} {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

// sourceCandidates returns the TypeScript files tsc tries for an import of
// p written with a JavaScript extension, in its order; nil for any other p.
func sourceCandidates(p string) []string {
	var exts []string
	stem := p
	for _, m := range []struct {
		js   string
		exts []string
	}{
		{".js", []string{".ts", ".tsx", ".d.ts"}},
		{".jsx", []string{".tsx"}},
		{".mjs", []string{".mts", ".d.mts"}},
		{".cjs", []string{".cts", ".d.cts"}},
	} {
		if s, ok := strings.CutSuffix(p, m.js); ok {
			stem, exts = s, m.exts
			break
		}
	}
	out := make([]string, 0, len(exts))
	for _, ext := range exts {
		out = append(out, stem+ext)
	}
	return out
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
