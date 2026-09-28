package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// minExternalModules is the smallest corpus SPEC.md 11.1 allows, not
// counting the standard library.
const minExternalModules = 20

// minTypeScriptRepos is the smallest TypeScript corpus: SPEC.md 13
// calibrates thresholds per language, from a corpus of its own.
const minTypeScriptRepos = 15

// Corpus languages. A corpus without a language key is a Go corpus.
const (
	languageGo         = "go"
	languageTypeScript = "typescript"
)

// stdlibModule is the corpus name of the standard library entry.
const stdlibModule = "std"

// errInvalidCorpus reports a corpus file that breaks the schema.
var errInvalidCorpus = errors.New("invalid corpus")

// Corpus is a reference corpus file: calibration/corpus.yaml for Go,
// calibration/corpus-typescript.yaml for TypeScript.
type Corpus struct {
	// Note explains the file; it carries no data.
	Note string `yaml:"note"`
	// Language is the language every entry is collected as: empty or go
	// for a Go corpus, typescript for a TypeScript one.
	Language string `yaml:"language,omitempty"`
	// Modules are the corpus entries, in collection order.
	Modules []Entry `yaml:"modules"`
}

// Entry is one corpus module.
type Entry struct {
	// Module is the module path.
	Module string `yaml:"module"`
	// Local marks the standard library, collected in-process from GOROOT
	// rather than cloned.
	Local bool `yaml:"local,omitempty"`
	// Repo is the git URL the module is cloned from; empty when Local.
	Repo string `yaml:"repo,omitempty"`
	// Commit is the full commit hash the module is pinned at; empty until
	// the --pin step fills it, and always empty when Local.
	Commit string `yaml:"commit"`
	// License is the SPDX identifier of the module's license.
	License string `yaml:"license"`
	// StarsOrDependents is the popularity evidence the entry was selected
	// on.
	StarsOrDependents string `yaml:"stars_or_dependents"`
	// Reason says why the module belongs in the corpus.
	Reason string `yaml:"reason"`
	// Modules are, in a TypeScript corpus only, the module roots
	// collected from the repository: slash paths relative to its root, "."
	// for the root itself, each a path.Match pattern that must match at
	// least one directory holding a package.json. A Go entry collects the
	// repository's root module and has none.
	Modules []string `yaml:"modules,omitempty"`
}

// IsTypeScript reports whether c is a TypeScript corpus.
func (c *Corpus) IsTypeScript() bool {
	return c.Language == languageTypeScript
}

// permissiveLicenses are the SPDX identifiers the selection criteria admit.
func permissiveLicenses() []string {
	return []string{"MIT", "BSD-2-Clause", "BSD-3-Clause", "Apache-2.0", "MPL-2.0"}
}

// LoadCorpus reads and validates the corpus file at path.
func LoadCorpus(path string) (*Corpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading corpus: %w", err)
	}
	c, err := ParseCorpus(data)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	return c, nil
}

// ParseCorpus decodes a corpus strictly, rejecting unknown keys, and
// validates it.
func ParseCorpus(data []byte) (*Corpus, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Corpus
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: empty document", errInvalidCorpus)
		}
		return nil, fmt.Errorf("decoding corpus: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks the corpus against the selection rules: exactly one local
// standard-library entry, at least minExternalModules cloned entries, unique
// module paths, a permissive license, a reason and popularity evidence on
// every entry, and a repo on every cloned entry. A commit may be empty (not
// yet pinned) but otherwise must be a full 40-character hex hash.
//
// A TypeScript corpus (language: typescript) instead has no local entry, at
// least minTypeScriptRepos cloned entries, and module roots on every entry.
func (c *Corpus) Validate() error {
	switch c.Language {
	case "", languageGo:
	case languageTypeScript:
		return c.validateTypeScript()
	default:
		return fmt.Errorf("%w: language %q is not %s or %s", errInvalidCorpus, c.Language, languageGo, languageTypeScript)
	}
	seen := make(map[string]bool, len(c.Modules))
	var locals, external int
	for i := range c.Modules {
		e := &c.Modules[i]
		if err := e.validate(); err != nil {
			return fmt.Errorf("%w: modules[%d]: %w", errInvalidCorpus, i, err)
		}
		if len(e.Modules) > 0 {
			return fmt.Errorf("%w: modules[%d]: %s: module roots are for a TypeScript corpus only", errInvalidCorpus, i, e.Module)
		}
		if seen[e.Module] {
			return fmt.Errorf("%w: modules[%d]: duplicate module %s", errInvalidCorpus, i, e.Module)
		}
		seen[e.Module] = true
		if e.Local {
			locals++
		} else {
			external++
		}
	}
	if locals != 1 {
		return fmt.Errorf("%w: want exactly one local (standard library) entry, got %d", errInvalidCorpus, locals)
	}
	if external < minExternalModules {
		return fmt.Errorf("%w: want at least %d cloned modules, got %d", errInvalidCorpus, minExternalModules, external)
	}
	return nil
}

// validateTypeScript checks a TypeScript corpus: every entry cloned, with
// the required fields and at least one well-formed module root, unique
// module paths, and at least minTypeScriptRepos entries.
func (c *Corpus) validateTypeScript() error {
	seen := make(map[string]bool, len(c.Modules))
	for i := range c.Modules {
		e := &c.Modules[i]
		if e.Local {
			return fmt.Errorf("%w: modules[%d]: %s: a TypeScript corpus has no local entry", errInvalidCorpus, i, e.Module)
		}
		if err := e.validate(); err != nil {
			return fmt.Errorf("%w: modules[%d]: %w", errInvalidCorpus, i, err)
		}
		if seen[e.Module] {
			return fmt.Errorf("%w: modules[%d]: duplicate module %s", errInvalidCorpus, i, e.Module)
		}
		seen[e.Module] = true
		if len(e.Modules) == 0 {
			return fmt.Errorf("%w: modules[%d]: %s: a TypeScript entry needs at least one module root", errInvalidCorpus, i, e.Module)
		}
		for _, p := range e.Modules {
			if err := validModuleRoot(p); err != nil {
				return fmt.Errorf("%w: modules[%d]: %s: module root %q: %w", errInvalidCorpus, i, e.Module, p, err)
			}
		}
	}
	if len(c.Modules) < minTypeScriptRepos {
		return fmt.Errorf("%w: want at least %d cloned repositories, got %d", errInvalidCorpus, minTypeScriptRepos, len(c.Modules))
	}
	return nil
}

// validModuleRoot checks a module root pattern: a clean relative slash
// path that stays inside the repository and is a valid path.Match pattern.
func validModuleRoot(p string) error {
	switch {
	case p == "":
		return errors.New("empty")
	case path.IsAbs(p) || p != path.Clean(p) || p == ".." || strings.HasPrefix(p, "../"):
		return errors.New("not a clean relative path inside the repository")
	}
	if _, err := path.Match(p, ""); err != nil {
		return fmt.Errorf("bad pattern: %w", err)
	}
	return nil
}

// validate checks one entry's fields.
func (e *Entry) validate() error {
	switch {
	case e.Module == "":
		return errors.New("module is required")
	case e.License == "":
		return fmt.Errorf("%s: license is required", e.Module)
	case !slices.Contains(permissiveLicenses(), e.License):
		return fmt.Errorf("%s: license %s is not one of %s", e.Module, e.License, strings.Join(permissiveLicenses(), ", "))
	case e.Reason == "":
		return fmt.Errorf("%s: reason is required", e.Module)
	case e.StarsOrDependents == "":
		return fmt.Errorf("%s: stars_or_dependents is required", e.Module)
	}
	if e.Local {
		if e.Module != stdlibModule {
			return fmt.Errorf("%s: only the standard library (%s) may be local", e.Module, stdlibModule)
		}
		if e.Repo != "" || e.Commit != "" {
			return fmt.Errorf("%s: a local entry has no repo or commit", e.Module)
		}
		return nil
	}
	if e.Repo == "" {
		return fmt.Errorf("%s: repo is required", e.Module)
	}
	if e.Commit != "" && !isCommitHash(e.Commit) {
		return fmt.Errorf("%s: commit %q is not a full 40-character hex hash", e.Module, e.Commit)
	}
	return nil
}

// isCommitHash reports whether s is a full lower-case SHA-1 hex hash.
func isCommitHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// resolveFunc resolves a repo URL to the commit its default branch points
// at.
type resolveFunc func(ctx context.Context, repo string) (string, error)

// lsRemoteHead resolves repo's HEAD with git ls-remote. It needs network.
func lsRemoteHead(ctx context.Context, repo string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "ls-remote", repo, "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote %s: %w", repo, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || !isCommitHash(fields[0]) {
		return "", fmt.Errorf("git ls-remote %s: no HEAD commit in %q", repo, strings.TrimSpace(string(out)))
	}
	return fields[0], nil
}

// PinCorpus fills every empty commit of the cloned entries in the corpus
// document data with the commit resolve returns for the entry's repo, and
// returns the updated document and the number of entries pinned. Each
// empty commit must be written as `commit: ""` on its own line; the edit
// replaces that quoted value in the text, so every other byte of the file
// (comments, blank lines, wrapping) is unchanged. Entries that already have
// a commit keep it. Nothing is returned when any resolve fails, so a
// partial pin never reaches the file.
func PinCorpus(ctx context.Context, data []byte, resolve resolveFunc) (out []byte, pinned int, err error) {
	if _, err := ParseCorpus(data); err != nil {
		return nil, 0, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, 0, fmt.Errorf("decoding corpus: %w", err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	// ParseCorpus accepted the document, so it is a mapping with a modules
	// list whose cloned entries all have a repo.
	modules := mappingValue(doc.Content[0], "modules")
	for _, item := range modules.Content {
		if v := mappingValue(item, "local"); v != nil && v.Value == "true" {
			continue
		}
		commit := mappingValue(item, "commit")
		module := mappingValue(item, "module").Value
		if commit == nil {
			return nil, 0, fmt.Errorf("%w: %s: pinning needs a commit: \"\" line", errInvalidCorpus, module)
		}
		if commit.Value != "" {
			continue
		}
		line, col := commit.Line-1, commit.Column-1
		if !strings.HasPrefix(lines[line][col:], `""`) {
			return nil, 0, fmt.Errorf("%w: %s: an empty commit must be written as \"\"", errInvalidCorpus, module)
		}
		sha, err := resolve(ctx, mappingValue(item, "repo").Value)
		if err != nil {
			return nil, 0, err
		}
		lines[line] = lines[line][:col] + `"` + sha + `"` + lines[line][col+2:]
		pinned++
	}
	out = []byte(strings.Join(lines, ""))
	if _, err := ParseCorpus(out); err != nil {
		return nil, 0, fmt.Errorf("pinned corpus: %w", err)
	}
	return out, pinned, nil
}

// mappingValue returns the value node of key in the mapping node m, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
