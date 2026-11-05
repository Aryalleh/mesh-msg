# Working on mesh-msg

- **Read [PROGRESS.md](PROGRESS.md) first.** It holds the current state, the next step and
  how to set up a fresh container.
- **Update PROGRESS.md after every step** (what changed, commit hash, what's next) and
  include it in the same commit, then push to `origin main`. Keep "Current state" and
  "Next step" accurate; append to "Log".
- No Claude attribution lines (Co-Authored-By etc.) in commits or PRs.
- Checks before committing: `go build ./... && go test ./...` and `make -C firmware test`.
