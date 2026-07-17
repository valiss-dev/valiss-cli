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

Pre-release. The full command tree from ADR 0021 is implemented over the
encrypted per-operator store of ADR 0020:

- `operator | account | user` — `add | list | show | remove | audit`
  (operator also `rotate`): the signing chain of nkey identities.
- `template` — `add | list | show | remove | audit`: per-operator,
  generation-stamped claimsets (extension grants, TTL, bearer, description).
- `token` — `mint | list | show | revoke`: issue user tokens (fail-closed on
  extensions), list, show, and revoke issuances.
- `creds export` — account, user, bundle, and bearer credential files.
- `allowlist` — `list | add | remove | export`: the local jti allowlist, with
  `export` producing exactly what servers consume.
- `store` — `init | info | config`; `inspect` — offline token decode.

The store is one encrypted SQLite file per operator under `~/.valiss/store/`,
keyed from `VALISS_STORAGE_KEY` (or an interactive prompt). Being pre-1.0, the
command surface may still change.

## License

MIT. See [LICENSE](LICENSE).
