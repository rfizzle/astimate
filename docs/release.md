# Cutting a release

A release is a `v*` tag pushed to `rfizzle/astimate`. `.github/workflows/release.yml`
waits for the commit's `ci` run to finish green, then runs goreleaser from
`.goreleaser.yaml`, which builds the archives, signs and notarizes the
darwin binaries, publishes the GitHub release and writes the Homebrew cask.
Nothing is built on a developer machine.

## The tag

Tag a commit on `master` that `ci` has run on. The version is the tag
without its `v`, so `v0.6.0` builds `astimate_0.6.0_<os>_<arch>.tar.gz`
and cask version `0.6.0`. Move the `[Unreleased]` entries in
`CHANGELOG.md` under a heading for the version in the same commit.

```sh
git tag -a v0.6.0 -m "v0.6.0"
git push origin v0.6.0
```

The `ci` job of the release workflow matches runs of the `ci` workflow by
the tag's commit, not by branch. It waits up to an hour for one to finish
and fails naming the run if none exists after five minutes, if the run
failed, or if it is still running at the deadline. A failed release is
re-run from the Actions tab once `ci` is green; the tag does not move.

## What goreleaser does

In order, from `.goreleaser.yaml`:

1. `make third-party-licenses` writes `THIRD_PARTY_LICENSES`, the license
   text of every module the binary links, through `go-licenses` at a
   pinned version (`scripts/third-party-licenses.sh`). The file is
   gitignored and generated fresh every release.
2. Builds `astimate` for linux and darwin on amd64 and arm64, with
   `CGO_ENABLED=0`, `-trimpath`, the grammar subset tags from the Makefile
   and the commit's timestamp as the build's, so the same tag builds the
   same bytes. The version, commit and date are linked in as `make build`
   links them.
3. Signs and notarizes the two darwin binaries (below).
4. Archives each build as `astimate_<version>_<os>_<arch>.tar.gz` with
   `astimate`, `README.md`, `LICENSE` and `THIRD_PARTY_LICENSES` at the
   root, and writes `checksums.txt`. These names are the contract
   `action/install.sh` downloads; the `release-snapshot` job in `ci.yml`
   checks them on every pull request.
5. Publishes the GitHub release with a changelog grouped by Conventional
   Commit type, then writes `Casks/astimate.rb` to `rfizzle/homebrew-tap`.

## Secrets

Repository secrets on `rfizzle/astimate`, read by `release.yml`:

| Secret | What it is | Without it |
| --- | --- | --- |
| `HOMEBREW_TAP_TOKEN` | A fine-grained token with contents read and write on `rfizzle/homebrew-tap`. | The cask step fails after the release is published. |
| `MACOS_SIGN_P12` | The Developer ID Application certificate and key as a base64 `.p12`. | The darwin binaries ship unsigned; the notarize step is skipped. |
| `MACOS_SIGN_PASSWORD` | The `.p12`'s password. | Signing fails. |
| `MACOS_NOTARY_ISSUER_ID` | The App Store Connect API key's issuer id. | Notarization fails. |
| `MACOS_NOTARY_KEY_ID` | The API key's id. | Notarization fails. |
| `MACOS_NOTARY_KEY` | The API key's `.p8` contents. | Notarization fails. |

Signing is enabled by `MACOS_SIGN_P12` alone (`enabled: isEnvSet`), so a
fork or a snapshot build without the secrets still builds. With it set,
the other four must be too. The same identity and tap serve `rfizzle/shhh`;
the secrets are the same values, set on each repository.

Signing and notarization run on the Linux runner through goreleaser's
`notarize` pipe (Quill): the binary is signed with the certificate and
submitted to Apple's notary service, and the release waits up to twenty
minutes for the verdict so a refusal fails here and not on a user's Mac.
A bare binary cannot carry a stapled ticket; Gatekeeper checks it online
the first time it runs, which is why the cask needs no quarantine hook.

## Afterwards

- The release page lists four archives and `checksums.txt`, and the
  changelog reads as the commits since the last tag.
- `Casks/astimate.rb` on `rfizzle/homebrew-tap` names the new version,
  with a sha256 for each darwin and linux archive.
- On a Mac, `brew update && brew upgrade --cask astimate` (or the install
  line in `README.md`) installs it, `astimate version` prints the tag, and
  `codesign -dv --verbose=2 "$(brew --prefix)/bin/astimate"` names the
  Developer ID and `spctl -a -vv -t install "$(which astimate)"` accepts it,
  when the signing secrets were set.
- The GitHub Action with `version: latest` installs the new archive
  (`action/install.sh`).
