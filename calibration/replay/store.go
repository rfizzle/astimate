package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Output file names inside the output directory.
const (
	packagesFile = "packages.jsonl"
	commitsFile  = "commits.jsonl"
	runFile      = "run.json"
)

// store appends rows to the output directory. A commit's package rows are
// written and synced before its commit row, so a commit row marks a commit
// whose rows are all on disk.
type store struct {
	packages *os.File
	commits  *os.File
	// rows are the commit rows commits.jsonl holds, and done their hashes.
	rows []commitRow
	done map[string]bool
	// packageRows counts the lines of packages.jsonl.
	packageRows int
}

// openStore prepares dir for appending, creating it when missing. It drops
// what an interrupted run left half written: a final line without a
// newline in either file, and the package rows of commits that have no
// commit row, so a resumed commit's rows are written exactly once.
func openStore(dir string) (s *store, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	s = &store{done: map[string]bool{}}
	defer func() {
		if err != nil {
			_ = s.close()
		}
	}()
	var lines [][]byte
	if s.commits, lines, err = openLog(filepath.Join(dir, commitsFile)); err != nil {
		return s, err
	}
	for i, line := range lines {
		var r commitRow
		if json.Unmarshal(line, &r) != nil || r.Commit == "" {
			return s, fmt.Errorf("%s line %d is not a commit row", commitsFile, i+1)
		}
		s.rows = append(s.rows, r)
		s.done[r.Commit] = true
	}
	if s.packages, lines, err = openLog(filepath.Join(dir, packagesFile)); err != nil {
		return s, err
	}
	return s, s.dropOrphans(lines)
}

// openLog opens the JSON-lines file at path for appending, creating it when
// missing, and returns its lines after truncating a final line an
// interrupted write cut short.
func openLog(path string) (*os.File, [][]byte, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", path, err)
	}
	data, err := io.ReadAll(f)
	keep := bytes.LastIndexByte(data, '\n') + 1
	if err == nil && keep < len(data) {
		err = f.Truncate(int64(keep))
	}
	if err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var lines [][]byte
	for line := range bytes.Lines(data[:keep]) {
		if len(bytes.TrimSpace(line)) > 0 {
			lines = append(lines, line)
		}
	}
	return f, lines, nil
}

// dropOrphans rewrites packages.jsonl, whose lines are lines, without the
// rows of commits that commits.jsonl does not record, when it has any.
func (s *store) dropOrphans(lines [][]byte) error {
	kept := make([][]byte, 0, len(lines))
	for i, line := range lines {
		var r struct {
			Commit string `json:"commit"`
		}
		if json.Unmarshal(line, &r) != nil {
			return fmt.Errorf("%s line %d is not a package row", packagesFile, i+1)
		}
		if s.done[r.Commit] {
			kept = append(kept, line)
		}
	}
	s.packageRows = len(kept)
	if len(kept) == len(lines) {
		return nil
	}
	if err := s.packages.Truncate(0); err != nil {
		return fmt.Errorf("dropping unfinished rows of %s: %w", packagesFile, err)
	}
	return writeSynced(s.packages, bytes.Join(kept, nil))
}

// record appends a commit's package rows, then its commit row, syncing
// each file, and marks the commit done.
func (s *store) record(rows []packageRow, cr *commitRow) error {
	data, err := encodeLines(rows)
	if err == nil {
		err = writeSynced(s.packages, data)
	}
	if err == nil {
		data, err = encodeLines([]*commitRow{cr})
	}
	if err == nil {
		err = writeSynced(s.commits, data)
	}
	if err != nil {
		return fmt.Errorf("recording %s: %w", cr.Commit, err)
	}
	s.rows = append(s.rows, *cr)
	s.done[cr.Commit] = true
	s.packageRows += len(rows)
	return nil
}

// encodeLines returns rows as JSON lines, with <module> left unescaped.
func encodeLines[R any](rows []R) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// writeSynced appends data to f and syncs it; nothing is written for no
// data.
func writeSynced(f *os.File, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", f.Name(), err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("syncing %s: %w", f.Name(), err)
	}
	return nil
}

// close closes the files that are open.
func (s *store) close() error {
	var errs []error
	for _, f := range []*os.File{s.packages, s.commits} {
		if f != nil {
			errs = append(errs, f.Close())
		}
	}
	return errors.Join(errs...)
}
