package metrics

import "errors"

// ErrUnknownPackage is returned, wrapped, by an Extractor's Extract or
// Details when the requested package identifier is not a non-test package
// of the module.
var ErrUnknownPackage = errors.New("unknown package")

// ErrUnknownTokenizer is returned, wrapped, by an Extractor when it was
// configured with a tokenizer other than "est" or "o200k".
var ErrUnknownTokenizer = errors.New("unknown tokenizer")
