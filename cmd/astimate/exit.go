package main

// Process exit codes, as defined in SPEC.md section 9.
const (
	// exitOK means success or the gate passed.
	exitOK = 0
	// exitUsage means the command line was invalid.
	exitUsage = 1
	// exitAnalysis means analysis failed before a verdict was reached.
	exitAnalysis = 2
	// exitGateFailed means the gate ran and at least one package failed it.
	exitGateFailed = 3
)
