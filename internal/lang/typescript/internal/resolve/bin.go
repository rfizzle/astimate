package resolve

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// BinDirs returns the directories, relative to root in slash form ("."
// for root), of the files the bin field of root's package.json names: a
// string, or an object of command names to strings. Those directories are
// the commands of the module, which a package of the same identifier is a
// composition root of (SPEC.md 6.5). A missing or unreadable manifest, a
// bin of another shape and a target outside root name none; nil when
// there is none.
func BinDirs(root string) map[string]bool {
	data, err := os.ReadFile(filepath.Join(root, ManifestName))
	if err != nil {
		return nil
	}
	var m struct {
		Bin json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(data, &m) != nil || len(m.Bin) == 0 {
		return nil
	}
	var one string
	var many map[string]string
	var targets []string
	switch {
	case json.Unmarshal(m.Bin, &one) == nil:
		targets = []string{one}
	case json.Unmarshal(m.Bin, &many) == nil:
		for _, t := range many {
			targets = append(targets, t)
		}
	}
	var dirs map[string]bool
	for _, t := range targets {
		if t == "" {
			continue
		}
		dir := path.Dir(path.Clean(filepath.ToSlash(t)))
		if dir == ".." || strings.HasPrefix(dir, "../") || path.IsAbs(dir) {
			continue
		}
		if dirs == nil {
			dirs = make(map[string]bool)
		}
		dirs[dir] = true
	}
	return dirs
}
