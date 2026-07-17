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

Pre-release scaffold. Only the root command and `--version` exist so far;
the command surface is being specified separately. Not yet usable for
trust-domain management.

## License

MIT. See [LICENSE](LICENSE).
