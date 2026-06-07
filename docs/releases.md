# Releases & auto-update

Hopper ships versioned releases via git tags, and clients/servers can update themselves to
the latest one.

## Cutting a release (maintainers)

```bash
git tag v0.2.0
git push origin v0.2.0
```

The `Release` GitHub Actions workflow (`.github/workflows/release.yml`) then:

1. cross-compiles `hopperd` and `hopper-agent` for `linux/amd64`, `linux/arm64`,
   `darwin/amd64`, `darwin/arm64` (via `make dist`), with the tag baked into the binary;
2. writes `checksums.txt` (SHA-256 of every artifact);
3. publishes a **GitHub Release** with the binaries + checksums and auto-generated notes;
4. builds and pushes the `hopperd` image to `ghcr.io/mcpeixoto/hopper:v0.2.0` and `:latest`.

Versioning is **SemVer**: `vMAJOR.MINOR.PATCH`. The running version is shown in each binary's
startup log and (for the agent) in the node status API.

## Auto-update

Both binaries can self-update. Enable it with:

```bash
HOPPER_AUTOUPDATE=1
HOPPER_UPDATE_INTERVAL_MIN=60   # check hourly (default)
```

On each check the updater:

1. fetches the latest release tag from the GitHub API,
2. compares it to the running version (SemVer); **dev builds are never auto-updated**, so a
   local `make build` is never clobbered,
3. downloads the asset for this exact OS/arch,
4. verifies its SHA-256 against `checksums.txt` from the same release,
5. atomically replaces the running executable (write-temp-then-rename) and **re-execs** into
   the new version.

For agents installed via systemd with `Restart=always`, the re-exec (or a clean exit) brings
up the new binary immediately. The result: **tag a release and your whole fleet converges**,
no manual SSH.

## Signed releases (Ed25519)

Hopper can cryptographically sign releases so the updater only installs binaries whose
checksums were signed by *your* key — protecting against a tampered release even if an
attacker can serve assets. It uses stdlib Ed25519 (no cosign/minisign dependency).

**Enable it once:**

1. Generate a keypair:
   ```bash
   go run ./cmd/hopper-sign keygen
   ```
2. Add the **private** key to the repo's GitHub Actions secrets as `HOPPER_SIGNING_KEY`.
3. Add the **public** key as the repo/Actions variable `HOPPER_SIGNING_PUBKEY`.

From then on, each release:
- the workflow signs `checksums.txt` → `checksums.txt.sig` with the private key;
- released binaries are built with the public key embedded
  (`-X .../internal/updater.SigningPublicKey=<pub>`).

When a public key is embedded, the updater **requires** a valid `checksums.txt.sig` before
replacing the binary — an unsigned or tampered release is refused. When no key is embedded
(the default), it falls back to checksum-only verification over HTTPS.

Verify a release by hand:
```bash
go run ./cmd/hopper-sign verify <PUBKEY> checksums.txt checksums.txt.sig
```

## Version convergence (fleet follows the server)

When `HOPPER_AUTOUPDATE=1`, **agents converge to the control plane's version** rather than
the latest GitHub tag: each agent polls `GET /api/version` and self-updates to that exact
release. Upgrade `hopperd`, and the fleet follows — no version skew, no per-node action.
(`hopperd` itself, with auto-update on, tracks the latest published release.)

## What's still trusted

Even signed, releases trust the CI pipeline and your GitHub account that holds the secret.
Keep the signing key in CI secrets only (never commit it). Pin to manual updates by leaving
`HOPPER_AUTOUPDATE` unset and upgrading yourself (download from Releases, or
`docker compose pull && up -d` for the server).
