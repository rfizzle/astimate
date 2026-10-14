# skipped

A module with one package that does not load. `remote` imports
`example.com/absent/client`, a module `go.mod` does not require, so go list
reports it missing without touching the network. `base` and `app` load;
`remote` is skipped, and its import of `base` still counts in `base`'s
`fan_in`. Its test file calls `base.New` and `(*base.Counter).Add`, which
no loaded package's test calls, so they read as tested only through the
syntactic references of a skipped package's tests; and `remote.digest` is a
copy of `base.Checksum`, a cross-package block that counts in `base`'s
`dup_blocks_cross_pkg` and on the module row while `remote` has no row.
