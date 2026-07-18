# Changelog

All notable changes to the valiss CLI are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims
to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

While the version is below 1.0 the command surface is not yet stable: breaking
changes may land in minor releases and are flagged **Breaking** below.

## [Unreleased]

### Added

- Initial CLI scaffold: the `valiss` root command with `--version` and
  configuration loading from the `~/.valiss/` dot-dir and `VALISS_*`
  environment variables (ADR 0017), built on cobra and viper (ADR 0019).
  Pre-release: no business subcommands yet.
- Release pipeline: `.goreleaser.yaml` cross-compiles the binary for
  darwin and linux on amd64 and arm64 (pure Go per ADR 0020), and a
  tag-triggered workflow gates on the full test suite before publishing a
  GitHub Release and pushing the formula to the `valiss-dev/homebrew-tap`
  Homebrew tap (ADR 0024). `go install valiss.dev/cli/valiss@latest` remains
  the toolchain-native path (ADR 0017).
- `inspect <token>`: offline decode of a token, printing the header, the
  registered claims, and the valiss body (type, epoch, bearer, extensions).
  The self-signature is checked and reported; no trust is evaluated
  (ADR 0021). `--json` for machine-readable output.
- Store foundation (ADR 0020): the credential store as one encrypted SQLite
  file per operator under `~/.valiss/store/`, on the `gosqlite.org` /
  `liteorm.org` stack. Encryption is mandatory (Adiantum), keyed from the
  `VALISS_STORAGE_KEY` environment variable or an interactive prompt through
  Argon2id. The schema is generation- and tombstone-aware from the first
  migration, and every operation writes the append-only audit journal.
- `store init | info | config`: create an operator store with a configurable
  audit-retention window, report its facts, and read or set its tunable
  configuration. `--json` on `info` and `config`.

### Notes

- The `gosqlite.org/vfs/cksm` page-checksum layer that ADR 0020 pairs with
  encryption is deferred: its VFS trips Go's `-d=checkptr` under the race
  detector that CI requires, and it is corruption defense-in-depth rather
  than authenticated encryption (gosqlite's own docs point to `vfs/vault`
  for integrity). Mandatory confidentiality ships now; tamper-evidence is
  tracked as follow-up.

[Unreleased]: https://github.com/valiss-dev/valiss-cli/commits/main
