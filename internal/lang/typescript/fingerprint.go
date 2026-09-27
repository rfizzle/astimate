package typescript

// Function fingerprints (SPEC.md 6.5 and 13.1).
//
// A function's fingerprint is a 64-bit FNV-1a hash of its body's
// duplication token stream, mixed in the same walk that emits the tokens:
// identifiers hash as one ID code, literals as one LIT code, and every other
// token as a hash of its kind, so the hash covers every keyword, operator
// and punctuation token of the body. Comments and zero-width tokens are not
// tokens, positions are not hashed, and semicolons are left out, so that a
// body written with and without them, relying on automatic semicolon
// insertion, hashes alike; an edit to comments, layout or semicolon style
// therefore leaves the fingerprint unchanged, and so does renaming a
// variable or changing a literal's value; any edit to control flow or
// operators changes it. Unlike the duplication stream, the tokens inside a template literal's
// substitutions are hashed too, since they may hold control flow; its text
// fragments and escapes are not. The one name the hash sees is the
// function's own, on a direct call to it, kept distinct as in the Go
// extractor. Kinds are hashed by name rather than by the module's interned
// token codes, which depend on the order files are scanned, so a function
// fingerprints the same in a baseline tree and at head.

const (
	// fpOffset and fpPrime are the 64-bit FNV-1a parameters.
	fpOffset uint64 = 14695981039346656037
	fpPrime  uint64 = 1099511628211
)

// Fixed token codes. Kind codes are 64-bit FNV-1a hashes of the kind name,
// which do not collide with these small values in practice.
const (
	fpIdent uint64 = iota + 1
	fpLit
	// fpSelfCall precedes the tokens of a call whose callee is an
	// identifier naming the enclosing function.
	fpSelfCall
)

// fpMix folds the token code v into the hash h as one FNV-1a step over a
// 64-bit word: tokens are the units, and one step per token keeps the walk
// cheap.
func fpMix(h, v uint64) uint64 {
	return (h ^ v) * fpPrime
}

// fpKind returns the fingerprint code of a token kind: the FNV-1a hash of
// its name.
func fpKind(kind string) uint64 {
	h := fpOffset
	for i := range len(kind) {
		h = (h ^ uint64(kind[i])) * fpPrime
	}
	return h
}
