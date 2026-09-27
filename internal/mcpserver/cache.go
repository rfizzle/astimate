package mcpserver

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rfizzle/astimate/internal/engine"
)

// session is the state the tools of one server share for its lifetime: the
// options and a cache of module loads and baselines per module root.
type session struct {
	opts Options

	mu    sync.Mutex
	roots map[string]*rootCache
}

// newSession returns a session over o with an empty cache.
func newSession(o Options) *session {
	return &session{opts: o, roots: make(map[string]*rootCache)}
}

// rootCache is the cached state of one module root. Its mutex serializes
// the tool calls on that root, so the cached extractor is never used by two
// calls at once and a rebuild never races a call still using the old one.
type rootCache struct {
	mu sync.Mutex
	// target holds the extractor, and through it the module load, reused
	// while the tree is unchanged.
	target *engine.Target
	// stamp is the tree's state when target was built.
	stamp treeStamp
	// baselines survive target rebuilds: a git baseline is keyed by its
	// merge-base commit and a file by its size and mtime, neither of which
	// an edit to the working tree changes.
	baselines *engine.BaselineCache
	// builds counts the targets built for this root; tests read it.
	builds int
}

// root returns the cache for the module root, creating it on first use.
func (s *session) root(root string) *rootCache {
	s.mu.Lock()
	defer s.mu.Unlock()
	rc, ok := s.roots[root]
	if !ok {
		rc = &rootCache{baselines: engine.NewBaselineCache()}
		s.roots[root] = rc
	}
	return rc
}

// loadTarget resolves dir to a Target like engine.LoadTarget and swaps in
// the cached extractor for its module root when the tree is unchanged since
// that extractor was built. When any Go source, go.mod or go.sum file or
// directory under the root has a newer mtime, or an entry was added or
// removed, the fresh Target, whose extractor has loaded nothing, replaces
// the cached one so the head is loaded again. The returned rootCache is
// locked; the caller unlocks it when done with the Target.
func (s *session) loadTarget(dir string) (*engine.Target, *rootCache, error) {
	fresh, err := engine.LoadTarget(dir, engine.TargetOptions{
		Config:  s.opts.Config,
		Version: s.opts.Version,
		Logger:  s.opts.logger(),
	})
	if err != nil {
		return nil, nil, err
	}
	rc := s.root(fresh.Mod.Root)
	rc.mu.Lock()
	// Stamp before any extraction, so an edit made while this call runs
	// shows up as a change on the next one.
	stamp, err := stampTree(fresh.Mod.Root)
	if err != nil {
		rc.mu.Unlock()
		return nil, nil, err
	}
	if rc.target == nil || !stamp.equal(rc.stamp) {
		rc.target, rc.stamp = fresh, stamp
		rc.builds++
	}
	t := *rc.target
	t.Dir, t.ImportPath = fresh.Dir, fresh.ImportPath
	return &t, rc, nil
}

// treeStamp summarizes a module tree for change detection: the newest
// modification time among its source files and directories, and how many
// there are. A directory's mtime moves when an entry is added, removed or
// renamed in it, and a file's when it is written.
type treeStamp struct {
	newest  time.Time
	entries int
}

// equal reports whether a and b describe the same tree state.
func (a treeStamp) equal(b treeStamp) bool {
	return a.entries == b.entries && a.newest.Equal(b.newest)
}

// stampTree walks the module tree at root and returns its stamp. It skips
// the directories the go tool ignores (names starting with "." or "_", and
// testdata) below the root. Nested modules are included, so an edit there
// rebuilds the target needlessly but never leaves it stale. An edit that
// keeps a file's mtime, as `touch -r` can, goes unnoticed.
func stampTree(root string) (treeStamp, error) {
	var st treeStamp
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
				return filepath.SkipDir
			}
		} else if !strings.HasSuffix(name, ".go") && name != "go.mod" && name != "go.sum" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		st.entries++
		if mt := info.ModTime(); mt.After(st.newest) {
			st.newest = mt
		}
		return nil
	})
	if err != nil {
		return treeStamp{}, fmt.Errorf("scanning %s for changes: %w", root, err)
	}
	return st, nil
}
