# Working on mesh-msg

- **Read [PROGRESS.md](PROGRESS.md) first.** It holds the current state, the next step and
  how to set up a fresh container.
- **Update PROGRESS.md after every step** (what changed, commit hash, what's next) and
  include it in the same commit, then push to `origin main`. Keep "Current state" and
  "Next step" accurate; append to "Log".
- **Everything ends up on `main`.** `main` is the only long-lived branch. If a session works
  on another branch (e.g. a `ccr-*` cloud-session branch), it merges or rebases that work
  onto `main` and pushes `main` before it finishes, then deletes its branch on origin. Tags
  are cut from `main` commits only. No work may be left only on a side branch.
- No Claude attribution lines (Co-Authored-By etc.) in commits or PRs.
- Checks before committing: `go build ./... && go test ./...` and `make -C firmware test`.
- **Every release gets a new version.** Release = push an annotated tag `vX.Y.Z[-alphaN]`
  (never reuse or move a tag; bump it even for a one-line fix). CI (`.github/workflows/android.yml`,
  don't rename it) stamps that version into the APKs (versionName = tag without `v`,
  versionCode = run number + 100, always growing, so phones update in place) and into
  `meshd`/`mesh` (`-X main.version`), and publishes the GitHub release. Record the tag in
  PROGRESS.md.
- **From the first beta on, every release (beta and stable) must ship the update check**
  (docs/roadmap.md B7, docs/decisions.md D20): the release job publishes a signed
  `release.json`, and the Android app and meshd show users that a newer version exists.
  No release without it. Before pushing a tag, add `docs/release-notes/<tag>.en.md` and
  `<tag>.fa.md` (the release job signs them into `release.json` and fails without them;
  it also needs the GitHub secret `RELEASE_SIGNING_KEY`, see docs/release-notes/README.md).
- If `android/` or `mobile/` changed: `scripts/build-android-core.sh` then
  `cd android && ./gradlew lintDebug testDebugUnitTest assembleDebug` (see PROGRESS.md for the toolchain).
