package metrics

// Forgetter is implemented by an Extractor that caches module loads across
// calls and can release one. Callers type-assert an Extractor to Forgetter
// after they are done with a module root, such as a temporary baseline
// worktree about to be deleted.
type Forgetter interface {
	// Forget drops whatever the extractor holds for the module at root, so
	// a later call for that root loads it afresh. A ModuleContext whose
	// Cache was filled before Forget keeps that load.
	Forget(root string)
}
