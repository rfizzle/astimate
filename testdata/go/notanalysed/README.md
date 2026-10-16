# notanalysed

A module with one package that is analysed and one directory that is not.
`greet` is an ordinary package. `gen` holds one Go file, a generator whose
`//go:build ignore` constraint excludes it from every build, so go list
drops the directory from `./...` without a word. The module report lists
`gen` under "Not analysed" with the reason "build constraints exclude all
Go files"; nothing gates it.
