// Package version exposes Hopper's build version. The value is overridden at
// build time via -ldflags "-X github.com/mcpeixoto/hopper/internal/version.Version=v0.1.0"
// (see the Makefile / release workflow). It underpins the self-update feature,
// which compares this against the latest published GitHub release tag.
package version

// Version is the semantic version of this build, e.g. "v0.1.0". "dev" means an
// unversioned local build.
var Version = "dev"

// Repo is the GitHub "owner/repo" the self-updater pulls releases from.
const Repo = "mcpeixoto/hopper"
