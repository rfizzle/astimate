package typescript

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// importKind is the class of one import specifier.
type importKind int

const (
	// importNone is an import counted in no class: one resolving inside
	// the module to a directory that is not a package.
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
// file system to tell a file target from a directory and caches the
// answers; it is not safe for concurrent use.
type resolver struct {
	root     string
	pkgs     map[string]*pkg
	cfg      tsconfig
	builtins map[string]bool
	stat     map[string]statKind
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
	return &resolver{root: root, pkgs: pkgs, cfg: cfg, builtins: nodeBuiltins(), stat: map[string]statKind{}}
}

// classify returns the class of spec imported from a file in the slash
// directory dir, relative to the module root:
//
//   - a relative specifier (".", "..", or starting with "./" or "../") or
//     one matching a tsconfig.json paths alias is resolved to a path; inside
//     the module it is internal when it lands in a package and counted
//     nowhere otherwise, and outside the module it is external under the
//     specifier itself;
//   - with a baseUrl, any other non-relative specifier naming a file or
//     directory under it is resolved there the same way, as tsc tries
//     baseUrl before node_modules;
//   - a specifier with the node: prefix, or whose first segment is a Node
//     built-in, is stdlib under the built-in's name;
//   - any other specifier is bare and external under its package name, the
//     first segment or, for a scoped package, the first two.
func (r *resolver) classify(dir, spec string) classified {
	if isRelative(spec) {
		return r.resolved(spec, []string{filepath.Join(r.root, filepath.FromSlash(path.Join(dir, spec)))})
	}
	if targets := r.cfg.match(spec); targets != nil {
		abs := make([]string, 0, len(targets))
		for _, t := range targets {
			abs = append(abs, filepath.Join(r.cfg.base, filepath.FromSlash(t)))
		}
		return r.resolved(spec, abs)
	}
	if r.cfg.baseURL != "" && !strings.HasPrefix(spec, "node:") {
		if p := filepath.Join(r.cfg.baseURL, filepath.FromSlash(spec)); r.kindOf(p) != statNone {
			return r.resolved(spec, []string{p})
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

// resolved classifies an import whose candidate absolute targets are abs,
// tried in order: the first that exists wins, else the first.
func (r *resolver) resolved(spec string, abs []string) classified {
	target, kind := abs[0], statNone
	for _, a := range abs {
		if k := r.kindOf(a); k != statNone {
			target, kind = a, k
			break
		}
	}
	rel, err := filepath.Rel(r.root, target)
	if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
		return classified{kind: importExternal, name: spec}
	}
	dir := filepath.ToSlash(rel)
	if kind != statDir {
		dir = path.Dir(dir)
	}
	id := packageDir(dir)
	if _, ok := r.pkgs[id]; !ok {
		return classified{kind: importNone}
	}
	return classified{kind: importInternal, name: id}
}

// kindOf reports what an import of the absolute path p refers to: a file
// when p itself or p with a TypeScript or JavaScript extension is a file,
// or when p ends in a JavaScript extension whose TypeScript counterpart
// replacing it is a file (.js to .ts, .tsx or .d.ts; .jsx to .tsx; .mjs to
// .mts or .d.mts; .cjs to .cts or .d.cts), as tsc resolves a module before
// a directory; a directory when p is one; nothing otherwise.
func (r *resolver) kindOf(p string) statKind {
	if k, ok := r.stat[p]; ok {
		return k
	}
	k := statNone
	for _, ext := range []string{"", ".ts", ".tsx", ".d.ts", ".js", ".jsx"} {
		fi, err := os.Stat(p + ext)
		if err != nil {
			continue
		}
		if fi.Mode().IsRegular() {
			k = statFile
			break
		}
		if ext == "" && fi.IsDir() {
			k = statDir
		}
	}
	if k != statFile {
		for _, c := range sourceCandidates(p) {
			if isFile(c) {
				k = statFile
				break
			}
		}
	}
	r.stat[p] = k
	return k
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
