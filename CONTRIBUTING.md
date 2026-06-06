# Contributing to Hopper

Thanks for helping out! Hopper aims to stay small, sharp, and dependency-light.

## Dev setup

```bash
git clone https://github.com/mcpeixoto/hopper && cd hopper
make build      # compile hopperd + hopper-agent
make test       # run the suite
```

You need **Go 1.22+**, and **Docker** if you want to run real jobs locally.

## Ground rules

- **Every new function/module gets a test.** Table-driven where it fits; `httptest` +
  in-memory SQLite for handler/store tests (see existing `*_test.go` for the patterns).
- **Docs stay in sync.** A behaviour change updates the relevant page in [`docs/`](docs/);
  a new feature adds one.
- **Keep dependencies minimal.** The whole server depends on one third-party module
  (`modernc.org/sqlite`); the agent depends on none. Prefer the standard library. Adding a
  dependency needs a justification in the PR.
- **Format and vet before pushing:** `make fmt vet` (CI runs `gofmt -l`, `go vet`, and
  `go test -race`).
- **Don't change existing tests** to make a change pass without explaining why in the PR.

## Commits & PRs

- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`,
  `docs:`, `test:`, `refactor:`, `chore:`.
- Keep PRs focused. Describe the *why*, not just the *what*.
- Link the issue it closes.

## Releasing (maintainers)

Releases are tag-driven:

```bash
git tag v0.2.0 && git push origin v0.2.0
```

CI cross-compiles the binaries, writes `checksums.txt`, publishes a GitHub Release, and
pushes the `hopperd` image to GHCR. Fleets with `HOPPER_AUTOUPDATE=1` pick it up. See
[docs/releases.md](docs/releases.md).

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). Be kind.
