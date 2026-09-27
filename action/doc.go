// Package action holds no Go code. The directory is the composite GitHub
// Action (action.yml) that installs astimate and runs `astimate check
// --format github`; the package exists so the tests of its shell scripts,
// install.sh and run.sh, run under `go test ./...`.
//
// testdata/releases mirrors a GitHub release's download layout and stands in
// for the network: the tests point install.sh at it with a file:// URL. Its
// archives hold a shell script named astimate, not a real binary, and its
// checksums.txt records the correct SHA-256 for the linux_amd64 archive, a
// wrong one for linux_arm64 and none for darwin_arm64. The archives are
// recorded bytes, not regenerated, so the recorded hashes stay valid.
package action
