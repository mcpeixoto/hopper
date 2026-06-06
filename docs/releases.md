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

## Trust model for updates

Releases are produced by CI, served by GitHub over HTTPS, and integrity-checked against
`checksums.txt` fetched over the same TLS channel. This protects against corruption and
truncation. It does **not** by itself protect against a compromised GitHub account or CI
pipeline. For higher assurance, sign artifacts with [minisign] or [cosign] and verify the
signature in the updater before replacing the binary (planned — see [roadmap.md](roadmap.md)).

Pin to manual updates by leaving `HOPPER_AUTOUPDATE` unset and upgrading binaries yourself
(download from Releases, or `docker compose pull && up -d` for the server).

[minisign]: https://jedisct1.github.io/minisign/
[cosign]: https://docs.sigstore.dev/cosign/overview/
