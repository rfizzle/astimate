#!/usr/bin/env bash
# Install astimate for the composite action in action.yml.
#
# Environment:
#   ASTIMATE_VERSION      "latest", a release tag such as "v0.3.0", or
#                         "source" to build the repository that contains
#                         this action with the local Go toolchain.
#   ACTION_DIR            this directory (github.action_path).
#   RUNNER_TEMP           install root; the binary lands in
#                         $RUNNER_TEMP/astimate/astimate.
#   RUNNER_OS, RUNNER_ARCH
#                         set by GitHub Actions (Linux|macOS, X64|ARM64).
#   GITHUB_PATH           when set, the install directory is appended to it.
#   ASTIMATE_RELEASE_URL  releases base URL; the tests point it at a
#                         recorded release under testdata/ with file://.
#
# A release install downloads astimate_<version>_<os>_<arch>.tar.gz and
# checksums.txt from the release, where <version> is the tag without its
# leading "v", and refuses to install when the archive's SHA-256 does not
# match its checksums.txt line.
set -euo pipefail

release_url="${ASTIMATE_RELEASE_URL:-https://github.com/rfizzle/astimate/releases}"

fail() {
	echo "::error title=astimate install::$*" >&2
	exit 1
}

# platform prints <os>_<arch> in release asset naming for the runner.
platform() {
	local os arch
	case "${RUNNER_OS:-$(uname -s)}" in
	Linux) os=linux ;;
	macOS | Darwin) os=darwin ;;
	*) fail "unsupported runner OS ${RUNNER_OS:-$(uname -s)}; use Linux or macOS" ;;
	esac
	case "${RUNNER_ARCH:-$(uname -m)}" in
	X64 | x86_64 | amd64) arch=amd64 ;;
	ARM64 | arm64 | aarch64) arch=arm64 ;;
	*) fail "unsupported runner architecture ${RUNNER_ARCH:-$(uname -m)}; use X64 or ARM64" ;;
	esac
	echo "${os}_${arch}"
}

# verify_checksum checks the SHA-256 of $dir/$asset against its line in
# $dir/checksums.txt. It returns non-zero when the line is missing or the
# hash differs; install_release treats either as fatal.
verify_checksum() {
	local dir=$1 asset=$2 line
	line="$(awk -v a="$asset" '$2 == a || $2 == "*" a { print $1 "  " a }' "$dir/checksums.txt")"
	if [ -z "$line" ]; then
		echo "no checksum for $asset in checksums.txt" >&2
		return 1
	fi
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$dir" && printf '%s\n' "$line" | sha256sum -c -)
	else
		(cd "$dir" && printf '%s\n' "$line" | shasum -a 256 -c -)
	fi
}

# resolve_latest prints the tag the releases/latest page redirects to.
resolve_latest() {
	local url
	url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$release_url/latest")" ||
		fail "cannot resolve the latest release at $release_url/latest"
	case "$url" in
	*/tag/*) echo "${url##*/tag/}" ;;
	*) fail "no published release at $release_url; set version to a tag or to source" ;;
	esac
}

install_release() {
	local dest=$1 tag=$2 asset tmp
	if [ "$tag" = latest ]; then
		tag="$(resolve_latest)"
	fi
	asset="astimate_${tag#v}_$(platform).tar.gz"
	tmp="$(mktemp -d "$RUNNER_TEMP/astimate-download.XXXXXX")"
	echo "downloading $asset from release $tag"
	curl -fsSL --retry 3 -o "$tmp/$asset" "$release_url/download/$tag/$asset" ||
		fail "cannot download $release_url/download/$tag/$asset"
	curl -fsSL --retry 3 -o "$tmp/checksums.txt" "$release_url/download/$tag/checksums.txt" ||
		fail "cannot download $release_url/download/$tag/checksums.txt"

	# A missing or mismatched checksum stops the install here, before
	# anything is extracted or put on PATH.
	if ! verify_checksum "$tmp" "$asset"; then
		fail "checksum verification failed for $asset; refusing to install"
	fi

	mkdir -p "$dest"
	tar -xzf "$tmp/$asset" -C "$dest"
	rm -rf "$tmp"
	[ -x "$dest/astimate" ] || fail "$asset has no executable astimate at its root"
}

# install_source builds ./cmd/astimate from the repository that contains
# this action, with the build tags and metadata `make build` uses.
install_source() {
	local dest=$1 root version commit date
	command -v go >/dev/null 2>&1 || fail "version: source needs Go on PATH; add actions/setup-go before this action"
	root="$(cd "$ACTION_DIR/.." && pwd)"
	version="$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)"
	commit="$(git -C "$root" rev-parse HEAD 2>/dev/null || echo unknown)"
	date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	mkdir -p "$dest"
	echo "building astimate $version from $root"
	(cd "$root" && go build \
		-tags 'grammar_subset grammar_subset_typescript grammar_subset_tsx' \
		-ldflags "-X main.buildVersion=$version -X main.buildCommit=$commit -X main.buildDate=$date" \
		-o "$dest/astimate" ./cmd/astimate)
}

main() {
	local version="${ASTIMATE_VERSION:-latest}" dest
	: "${RUNNER_TEMP:?RUNNER_TEMP must be set}"
	: "${ACTION_DIR:?ACTION_DIR must be set}"
	dest="$RUNNER_TEMP/astimate"
	if [ "$version" = source ]; then
		install_source "$dest"
	else
		install_release "$dest" "$version"
	fi
	if [ -n "${GITHUB_PATH:-}" ]; then
		echo "$dest" >>"$GITHUB_PATH"
	fi
	echo "installed $dest/astimate"
}

main "$@"
