package dup

import (
	"encoding/json"
	"go/token"
	"os"
	"slices"
	"strconv"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
)

// kindMeasure is what TestMeasureBlockKinds writes: the blocks of every
// package and the module's cross-package blocks, each with its kind.
type kindMeasure struct {
	Module   string                    `json:"module"`
	Packages map[string]kindMeasurePkg `json:"packages"`
	// Cross is nil for the standard library, which has no cross pass.
	Cross []kindMeasureBlock `json:"cross"`
}

// kindMeasurePkg is one package's blocks.
type kindMeasurePkg struct {
	Blocks int                `json:"dup_blocks"`
	Code   int                `json:"dup_blocks_code"`
	List   []kindMeasureBlock `json:"blocks"`
}

// kindMeasureBlock is one block: its kind, its anchors when known, and
// its occurrences rendered file:start-end.
type kindMeasureBlock struct {
	Kind      string    `json:"kind"`
	Anchors   [2]uint64 `json:"anchors"`
	Locations []string  `json:"locations"`
}

// TestMeasureBlockKinds measures the duplicate block kinds of SPEC.md 6.3
// over the module in $ASTIMATE_DUPKINDS_DIR (pattern ./..., or std for the
// standard library's src directory) and writes them as JSON to
// $ASTIMATE_DUPKINDS_OUT, for calibration/notes/dup-block-kinds-2026-10-14.md.
// It loads syntax only, as the duplication pass reads nothing else, and
// keeps every package that parsed. It runs only when both are set.
func TestMeasureBlockKinds(t *testing.T) {
	dir, out := os.Getenv("ASTIMATE_DUPKINDS_DIR"), os.Getenv("ASTIMATE_DUPKINDS_OUT")
	if dir == "" || out == "" {
		t.Skip("set ASTIMATE_DUPKINDS_DIR and ASTIMATE_DUPKINDS_OUT to measure")
	}
	modPath, err := load.ReadModulePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	pattern := "./..."
	if modPath == load.StdModulePath {
		pattern = "std"
	}
	m := &load.Module{Path: modPath, Pkgs: make(map[string]*packages.Package)}
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax, Dir: dir, Fset: token.NewFileSet()}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		t.Fatal(err)
	}
	m.Fset = cfg.Fset
	for _, p := range pkgs {
		if len(p.Syntax) == 0 || len(p.Syntax) != len(p.GoFiles) {
			continue
		}
		m.Paths = append(m.Paths, p.PkgPath)
		m.Pkgs[p.PkgPath] = p
	}
	slices.Sort(m.Paths)
	res := kindMeasure{Module: modPath, Packages: make(map[string]kindMeasurePkg)}
	src := load.NewFileCache(load.OSFiles{})
	for _, ip := range m.Paths {
		p := m.Pkgs[ip]
		r, err := Package(m, p, src, 1, DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		_, blocks := duptok.Details(r.Groups(), func(f string) string { return load.RelFile(p.Dir, f) })
		row := kindMeasurePkg{Blocks: r.Blocks, Code: r.Code}
		for _, b := range blocks {
			row.List = append(row.List, kindMeasureBlock{Kind: b.Kind, Anchors: b.Anchors, Locations: b.Locations})
		}
		res.Packages[ip] = row
	}
	if Applies(m) {
		c, err := computeCross(m, src, DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range c.Blocks {
			mb := kindMeasureBlock{Kind: b.Kind}
			for _, o := range b.Occurrences {
				mb.Locations = append(mb.Locations, o.File+":"+strconv.Itoa(o.StartLine)+"-"+strconv.Itoa(o.EndLine))
			}
			res.Cross = append(res.Cross, mb)
		}
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("measured %d packages of %s", len(m.Paths), modPath)
}
