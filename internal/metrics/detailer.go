package metrics

import (
	"context"

	"github.com/rfizzle/astimate/internal/metrics/inventory"
)

// Detailer is an optional interface an Extractor implements when it can name
// what some of its counts refer to. Callers type-assert an Extractor to it
// and fall back to counts alone when the assertion fails.
type Detailer interface {
	// Details returns the details of pkg. They are valid only after Extract
	// for pkg on the same mod; an implementation may call Extract itself
	// when nothing is recorded for pkg yet. It fails for a package Extract
	// would reject.
	Details(ctx context.Context, mod *ModuleContext, pkg string) (inventory.Details, error)
}

// ModuleDetailer is an optional interface an Extractor that implements
// ModuleMetrics implements when it can name what the module row's counts
// refer to. Callers type-assert an Extractor to it and fall back to counts
// alone when the assertion fails.
type ModuleDetailer interface {
	// ModuleDetails returns the details of the module row of the module
	// described by mod: today only CrossBlocks, every cross-package block
	// behind the row's dup_blocks_cross_pkg. It fails when ModuleRow would.
	ModuleDetails(ctx context.Context, mod *ModuleContext) (inventory.Details, error)
}

// FunctionLister is an optional interface an Extractor implements when it
// can list the functions of a package with fingerprints. Without it,
// changed_func_cognitive_max stays null and its rule is skipped. Callers
// type-assert an Extractor to it.
type FunctionLister interface {
	// Functions returns the functions and methods of pkg's non-test files,
	// in declaration order. Like Detailer.Details, the result reflects the
	// most recent Extract of pkg on the same mod, and an implementation may
	// call Extract itself when nothing is recorded yet.
	Functions(ctx context.Context, mod *ModuleContext, pkg string) ([]inventory.FunctionInfo, error)
}
