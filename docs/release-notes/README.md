# Release notes

Every tag `vX.Y.Z[-preN]` needs `vX.Y.Z[-preN].en.md` and `vX.Y.Z[-preN].fa.md` here
before it is pushed: the release job puts both into the signed `release.json`
(roadmap B7) and fails without them. Keep them short; apps show them in the update
banner and notification.

`min-supported` (optional, one version per line, the last line counts): installs older
than that version are told the update is required (a security release). Leave it out
or empty when every version is still supported.

## Release key

`release.json` is signed with an Ed25519 key whose public half is
`core/update.ReleaseKeyHex`. The private half (a 32-byte hex seed) is never in the
repository: it is the GitHub Actions secret `RELEASE_SIGNING_KEY` and the owner keeps an
offline copy. `go run ./tools/release-manifest keygen FILE` makes a new one (then update
`ReleaseKeyHex`; apps built with the old key ignore manifests signed with the new one, so
rotate only with a release signed by the old key first).
