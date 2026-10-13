# skipped

A module with one package that does not load. `remote` imports
`example.com/absent/client`, a module `go.mod` does not require, so go list
reports it missing without touching the network. `base` and `app` load;
`remote` is skipped, and its import of `base` still counts in `base`'s
`fan_in`.
