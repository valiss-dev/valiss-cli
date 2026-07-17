# valiss-cli

`valiss` is the command-line tool for operating a [valiss](https://github.com/valiss-dev/valiss-go)
trust domain: minting operator, account, and user tokens, managing keys and
creds files, and revocation.

## Install

```sh
go install valiss.dev/cli/valiss@latest
```

The installed binary is named `valiss`.

## Status

Pre-release scaffold. The full command tree from ADR 0021 (operator,
account, user, template, token, creds, allowlist, store, inspect) is wired
with its flags, help text, and argument validation, but every command body
is a stub that returns a not-implemented error. Implementations arrive with
the store layer. Not yet usable for trust-domain management.

## License

MIT. See [LICENSE](LICENSE).
