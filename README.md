# store

`store` keeps files, folders, and text in a secure vault so other computers
with access to the same vault can pull them. 1Password is the first supported
provider; the core is provider-neutral so Bitwarden, LastPass, and others can
be added as adapters.

```sh
# Computer A
store keep ~/.env --name root-env --tag project/api

# Computer B
store pull root-env            # asks where to save it
store pull root-env --to ~/.env
```

Transfers are explicit snapshots, not background sync.

## Install

Requires Go 1.26+ and the [1Password CLI](https://developer.1password.com/docs/cli/get-started/) (`op`).

```sh
go install github.com/kshehadeh/store/cmd/store@latest
# or, from a checkout:
go build -o ~/bin/store ./cmd/store
```

For this private repository, Git must be authenticated with GitHub. Set
`GOPRIVATE=github.com/kshehadeh/store` when installing by module path so Go
does not query the public module proxy or checksum database. Building from
a checkout does not need this setting.

### Run without installing

From a checkout, `go run` compiles into Go's build cache and runs the CLI in
one step, so there is no binary to build or put on your `PATH`. Put `store`'s
own arguments after the package path:

```sh
cd selfstore
go run ./cmd/store status
go run ./cmd/store keep ~/.env --name root-env --tag project/api
go run ./cmd/store pull root-env --to ~/.env
```

For everyday use, build a local binary and point your alias at it:

```sh
go -C ~/dev/karim/selfstore build -o dist/store ./cmd/store
unalias store 2>/dev/null || true
alias store="$HOME/dev/karim/selfstore/dist/store"
```

The binary runs in your current directory, so relative paths work normally.
Re-run the build after changing the source; Go reuses its build cache.
Unlike `go run`, the binary does not print an extra `exit status 1` line when
a command fails. It still returns a nonzero exit code and prints useful errors.

## Set up each computer

1. Sign in to 1Password for the CLI. The simplest way is to turn on
   **Settings > Developer > Integrate with 1Password CLI** in the 1Password
   app. `eval $(op signin)` and `OP_SERVICE_ACCOUNT_TOKEN` also work.
2. Run `store setup` and choose the account and vault. **Choose the same vault
   on every computer.** You can also pass choices directly:
   `store setup --vault Shared --account my.1password.com`.
3. Run `store status` to check configuration, sign-in, and vault access.
   It never writes to the vault and exits nonzero if something is wrong.

Setup saves only identifiers (provider, account, vault) to
`~/.config/store/config.json` (or `$XDG_CONFIG_HOME/store/config.json`;
override with `STORE_CONFIG`). No passwords or tokens are stored.

### Terminal controls

Interactive account, vault, item, and field selection uses Bubble Tea with
Bubbles' searchable list. Move with the arrow keys, press `/` to filter, and
press Enter to confirm a filter, then Enter again to select. Esc clears a
filter or cancels; Ctrl-C cancels at any time.

Destination and hidden-text prompts use Bubbles' text input. Enter confirms
(an empty destination uses the displayed default); Esc or Ctrl-C cancels.
Hidden text is never echoed, including its length. All interactive UI goes
to stderr so pulled content on stdout stays unchanged.

Scripts should use explicit flags such as `--vault`, `--id`, `--field`, and
`--to`. Piped text still works without a terminal. `STORE_INTERACTIVE=1`
enables simple line-based prompts for testing when stdin is not a terminal.

## Commands

| Command | What it does |
|---|---|
| `store keep PATH --name NAME [--tag T]...` | Store a file byte-for-byte, or a folder as a ZIP (dotfiles and empty directories included; symlinks and special files are refused). |
| `store keep --text --name NAME [--tag T]...` | Store text from stdin (`pbpaste \| store keep --text --name token`) or from a hidden prompt. |
| `store list [--tag T]... [--json]` | List discoverable items with TYPE, HOST, and SOURCE columns read from their `store/meta/` tags; TAGS shows only your labels (all `meta/` tags are left out; use `--json` to see them). Never reads secret values. |
| `store pull` | Choose from all discoverable items, most recently updated first. |
| `store pull NAME` | Pull by exact name. |
| `store pull --tag T [--tag T]...` | Pull by tags; choose from a list when several match. |
| `store pull --id ID` | Pull a specific item (use this in scripts when a query is ambiguous). |

Pull options:

- With no name, tags, or ID, the picker shows the newest items first using
  the vault's last-update time, including items kept again and items tagged
  by hand. Items without a timestamp appear last. A terminal is required,
  and even a single item asks for confirmation.
- Text, Secure Notes, and fields are written to stdout exactly as stored (no
  added newline), so they can be piped. Prompts go to stderr.
- Files and folders need a destination: `--to PATH`, or you are asked (the
  default is the original name in the current directory). If the destination is
  an existing directory, the item is placed inside it.
- Folders are restored as a directory tree; `--as-zip` saves the archive.
- `--to` also works for text, saving it to a file.
- `store` never overwrites an existing file or folder, and never writes to the
  path recorded as the item's source. Downloads are staged next to the
  destination with private permissions and moved into place only when
  complete.

## Tags

Everything `store` does is based on 1Password tags under `store/`.

- **Any item in the configured vault with a `store/` tag is discoverable**,
  whatever its category and whoever created it. Add `store/project/api` to an
  existing Login in the 1Password app and it shows up in
  `store list --tag project/api` on every computer.
- `--tag project/api` means `store/project/api`; the full spelling also works.
  Filters match whole segments: `project` matches `project/api` but not
  `projectile`. Repeated `--tag` filters must all match.
- Tags under `store/meta/` are set automatically and can be filtered like any
  other tag:

  | Tag | Meaning |
  |---|---|
  | `store/meta/managed/v1` | Created by `store keep`; `keep` may update it. |
  | `store/meta/type/{file,folder,text}` | Content type. |
  | `store/meta/host/<hostname>` | Computer that last kept it (override with `STORE_HOSTNAME`). |
  | `store/meta/os/<os>` | Operating system of that computer. |
  | `store/meta/source/<path>` | Directory the file or folder came from (informational only). |

  Example: `store list --tag meta/host/laptop --tag meta/type/folder`.

### Ownership

Discoverable does not mean writable. `store keep` only updates a Document that
has `store/meta/managed/v1`. If the name belongs to any other item, or to
several items, it stops instead of overwriting or creating a duplicate. When it
updates an item it replaces the content and the `store/meta/` tags, and keeps
every other tag, including ones you added in 1Password.

### Items you tag by hand

| Category | `store pull` returns |
|---|---|
| Document | The file. A ZIP stays a file unless it is also tagged `store/meta/type/folder`. |
| Secure Note | The note text. |
| Login, API Credential, and others | One field: chosen from a list, or with `--field password`, `--field Section.label`, or a field ID. `store` never prints a whole item or picks a field for you. |

## Limits

- Folder restores refuse archives with more than 100,000 entries or more than
  4 GiB of content, and any entry that is absolute, escapes the destination,
  or is a link or special file.
- Restored files keep their owner permission bits (for example, executables)
  and are never readable by group or others.
- 1Password limits file storage by plan; very large folders may be rejected by
  1Password itself.

## Development

### Project layout

The module is `github.com/kshehadeh/store`. The executable stays thin, and
implementation packages remain internal rather than becoming a public API.

```text
cmd/store/                  CLI entry point and end-to-end tests
internal/cli/               Cobra root and one file per command
internal/app/               Command behavior and terminal UI
internal/provider/          Vault interface and 1Password adapter
internal/providers/         Supported-provider registry
internal/config/            Local provider and vault configuration
internal/tags/              Discovery, labels, and metadata conventions
internal/archive/           ZIP packing and safe extraction
internal/restore/           Destination checks and staged publication
internal/testsupport/fakeop/ Simulated 1Password CLI for tests
.github/workflows/ci.yml     macOS and Linux checks
```

```sh
gofmt -w cmd internal
go test -race ./...
go vet ./...
go build -o dist/store ./cmd/store
```

CI runs formatting checks, module verification, race-enabled tests, vet,
and a build on macOS and Linux. Its tests use only the simulated vault.

The end-to-end tests in `cmd/store` build the real `store` binary and run it
against `internal/testsupport/fakeop`, a stand-in for `op` backed by a JSON
file. They simulate two computers with separate configurations sharing one
vault. They never call the real `op` or touch your vault.

### Smoke test against a real, disposable vault

Use a vault created for this purpose, not one holding real data.

```sh
op vault create store-smoke-test
store setup --vault store-smoke-test
store status

echo "hello" > /tmp/store-smoke.txt
store keep /tmp/store-smoke.txt --name smoke-file --tag smoke
echo "v2" > /tmp/store-smoke.txt
store keep /tmp/store-smoke.txt --name smoke-file     # Updated, same item
printf 'secret' | store keep --text --name smoke-text --tag smoke
store list --tag smoke
store pull smoke-text                                 # prints: secret
store pull smoke-file --to /tmp/store-smoke-pulled.txt
cat /tmp/store-smoke-pulled.txt                       # prints: v2

op vault delete store-smoke-test
rm /tmp/store-smoke.txt /tmp/store-smoke-pulled.txt
```

Run `store setup` again afterwards to point at your real vault.

## Adding a provider

Implement `provider.Provider` (`internal/provider`) and a `provider.Definition`
with `Setup`, `Open`, and `Describe`, then add it to
`internal/providers/providers.go`. The adapter maps the vault's native items,
tags, and IDs onto store's model; tag semantics, selection, ZIP handling, and
safe restoration stay in the core.
