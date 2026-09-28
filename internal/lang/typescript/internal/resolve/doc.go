// Package resolve finds the files of a TypeScript module and resolves its
// import specifiers the way tsc does under moduleResolution bundler
// (SPEC.md 13.1): which directories are packages and which files are
// tests, the compiler options of the root tsconfig.json with its extends
// chain applied (paths, baseUrl, allowJs, checkJs, resolveJsonModule),
// the package.json entries of a directory, and whether a specifier is
// internal, external or a Node built-in. It reads configuration and stats
// the file system but never reads a source file; the typescript extractor
// composes it with the per-file pass in package inspect.
package resolve
