# AGENTS.md

Guidance for AI coding agents working in this repository.

## What this is

`valiss` is the command-line tool for operating a valiss trust domain
(the library lives in [valiss-go](https://github.com/valiss-dev/valiss-go)):
minting operator/account/user tokens, managing keys and creds files, and
revocation. Module: `valiss.dev/cli/valiss`, root package `main`, binary
`valiss` (ADR 0017). Built on cobra + viper (ADR 0019). Single module, no
`cmd/` nesting: `main` sits at the repository root so
`go install valiss.dev/cli/valiss@latest` installs a binary named `valiss`.

Pre-release scaffold: the command tree specified by ADR 0021 is wired
(nouns, verbs, flags, help, argument validation), but every command body is
a stub returning a shared not-implemented error. Bodies land with the store
layer (ADR 0020). Follow ADR 0021 for the surface; do not invent commands or
flags it does not specify.

Command files are one per noun (`command_<noun>.go`); shared helpers
(`errNotImplemented`, path validators, flag helpers) live in `command.go`.
Entity lifecycle counters are "generation"/"gen", never "version".

Plain Go toolchain: no Makefile or lint config.

## Commands

```sh
go build ./...         # build everything
go test ./...          # full test suite
go vet ./...
go mod tidy            # sync dependencies
go run . --version     # run the CLI
```

## Conventions

- Config conventions (ADR 0017): the `~/.valiss/` dot-dir and `VALISS_*`
  environment variables, bound through viper in `initConfig`.
- Error messages are prefixed `valiss:`.
- The full spf13 suite (cobra, viper, pflag) is the CLI's parsing and
  configuration stack (ADR 0019); keep it confined to this distributable
  and out of library modules.
