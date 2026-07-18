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

Pre-release, under active implementation. The full command tree from ADR
0021 (operator, account, user, template, token, creds, allowlist, store,
inspect) is wired with its flags, help text, and argument validation. The
store foundation (ADR 0020) is in place — one encrypted SQLite file per
operator — and the following are implemented:

- `inspect <token>` — offline token decode.
- `store init | info | config` — create, inspect, and tune an operator store.

The remaining entity, token, template, creds, and allowlist verbs are wired
but still return a not-implemented error; their bodies land verb-family by
verb-family on top of this foundation. Not yet usable end to end for
trust-domain management.

## License

MIT. See [LICENSE](LICENSE).
