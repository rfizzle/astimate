package typescript

import (
	"io"
	"os"

	"github.com/rfizzle/astimate/internal/lang/typescript/internal/inspect"
)

// generatedHead is how much of a file IsGeneratedFile reads: a generated
// header is a few lines, and a comment block longer than this is no header.
const generatedHead = 64 << 10

// IsGenerated implements metrics.GeneratedClassifier with IsGeneratedFile.
func (e *Extractor) IsGenerated(path string) bool { return IsGeneratedFile(path) }

// IsGeneratedFile reports whether the TypeScript file at path carries a
// generated-code marker in its leading comment block, the rule
// generated_files counts by (SPEC.md 6.5). Only the head of the file is
// read. A file that cannot be read, such as one no longer in the tree, is
// not generated.
func IsGeneratedFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	head, err := io.ReadAll(io.LimitReader(f, generatedHead))
	if err != nil {
		return false
	}
	return inspect.IsGenerated(head)
}
