# Releasing the valiss CLI

Operator runbook for cutting a valiss CLI release. The pipeline is
goreleaser, triggered by pushing a `vX.Y.Z` tag; see `.goreleaser.yaml` and
`.github/workflows/release.yaml`.

## Distribution channels

- **Homebrew tap** (ADR 0024): `brew install valiss-dev/tap/valiss`. The
  formula installs the prebuilt GitHub Release binary; the release pipeline
  pushes it to `valiss-dev/homebrew-tap`.
- **`go install`** (ADR 0017), unchanged and toolchain-native:
  `go install valiss.dev/cli/valiss@latest` (or `@vX.Y.Z`).

## Versioning

Semantic Versioning. While below 1.0 the command surface is not stable:
breaking changes may land in minor releases and are flagged **Breaking** in
the changelog. `CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
same as valiss-go.

## How release notes are produced

`CHANGELOG.md` is the single source of truth. goreleaser's git-based
changelog is disabled (`changelog.disable: true`). On a tag push the release
workflow extracts the tag's `## [X.Y.Z]` section from `CHANGELOG.md` and
passes it to goreleaser via `--release-notes`, so the GitHub Release body
matches the changelog verbatim. Nothing is generated from commit messages.

## Pre-flight

1. The full suite is green on `main` (`go build ./...`, `go vet ./...`,
   `go test -race ./...`; the release aborts on a red suite anyway, see
   below).
2. `CHANGELOG.md` `[Unreleased]` lists everything shipping in this release,
   in Keep a Changelog categories (Added / Changed / Deprecated / Removed /
   Fixed / Security).
3. You are on `main`, up to date, working tree clean.

## Finalize the changelog

Edit `CHANGELOG.md` exactly as valiss-go does (see its commit
`f793667`, "docs: cut CHANGELOG 0.13.1"):

1. Rename the `## [Unreleased]` heading to `## [X.Y.Z] - YYYY-MM-DD`
   (today's date), and add a fresh empty `## [Unreleased]` above it.
2. Update the reference links at the bottom of the file:
   - Point `[Unreleased]` at `compare/vX.Y.Z...HEAD`.
   - Add a line for the new version. For the first release:
     `[0.1.0]: https://github.com/valiss-dev/valiss-cli/releases/tag/v0.1.0`.
     For later releases:
     `[X.Y.Z]: https://github.com/valiss-dev/valiss-cli/compare/vPREV...vX.Y.Z`.

Commit with the valiss-go message convention:

```sh
git commit -am "docs: cut CHANGELOG X.Y.Z"
git push origin main
```

## Tag and push

```sh
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

## What CI does

The tag push triggers `.github/workflows/release.yaml`:

1. **test gate** — calls `test.yaml` via `workflow_call`. If the suite is
   red, the release stops here; nothing is published.
2. **goreleaser** — cross-compiles darwin/linux on amd64/arm64
   (CGO_ENABLED=0, trimpath, version stamped via
   `-X main.version=X.Y.Z`), builds archives + `checksums.txt`, and creates
   the **GitHub Release** with the notes extracted from `CHANGELOG.md`.
3. **tap formula** — pushes the updated formula to `valiss-dev/homebrew-tap`.
   This step self-skips until `HOMEBREW_TAP_TOKEN` is provisioned (see
   Prerequisites); the GitHub Release still publishes without it.

## Verify

```sh
gh release view vX.Y.Z --repo valiss-dev/valiss-cli
go install valiss.dev/cli/valiss@vX.Y.Z && valiss --version   # prints "valiss version X.Y.Z"
brew install valiss-dev/tap/valiss && valiss --version        # once the tap step is live
```

## Rollback

A bad release is corrected by deleting the release and tag, then re-cutting:

```sh
gh release delete vX.Y.Z --repo valiss-dev/valiss-cli --yes
git push origin --delete vX.Y.Z
git tag -d vX.Y.Z
```

- **Tap**: the formula lives in `valiss-dev/homebrew-tap`. Revert the
  offending formula commit there (`git revert`) or push a corrected formula;
  the org owns the tap outright (ADR 0024), no external review in the loop.
- **`go install`**: module versions are immutable in the proxy. You cannot
  unpublish; supersede with a higher patch. Retracting a version is possible
  via a `retract` directive in a follow-up release.

## Prerequisites for tap publishing (one-time, not yet provisioned)

The tap step is wired but inert until two pieces of infrastructure exist.
Until then, releases publish binaries and the GitHub Release normally, and
the formula step self-skips.

1. **The tap repository `valiss-dev/homebrew-tap`.** Not yet created. It is
   declared in `infra` the same way as its siblings (ADR 0024), for example
   in `infra/main_github.tf`:

   ```hcl
   resource "github_repository" "homebrew_tap" {
     name         = "homebrew-tap"
     description  = "Homebrew tap for the valiss CLI (ADR 0024)"
     visibility   = "public"
     has_issues   = true
     has_wiki     = false
     has_projects = false

     lifecycle {
       ignore_changes = [has_downloads]
     }
   }
   ```

   Apply per the infra runbook (`make tofu/plan`, `make tofu/apply`).

2. **A cross-repo token as the `HOMEBREW_TAP_TOKEN` Actions secret on
   `valiss-dev/valiss-cli`.** The default `GITHUB_TOKEN` cannot push to
   another repository, so goreleaser needs a token with **`contents: write`
   on `valiss-dev/homebrew-tap`** (a fine-grained PAT scoped to that repo, or
   a GitHub App installation token). No such token exists in the `valiss.dev`
   1Password vault today, and org Actions secrets are not currently
   Terraform-managed, so this is net-new: create the token, store it in the
   vault, and set it as the `HOMEBREW_TAP_TOKEN` repository secret before the
   first tap-publishing release.

## Notes

- `goreleaser check` reports the `brews` block as deprecated (goreleaser now
  prefers `homebrew_casks`). The formula mechanism is retained deliberately:
  Homebrew Casks are macOS-only, and ADR 0024 requires Linux coverage via
  Linuxbrew, which the formula provides. The config is otherwise valid.
