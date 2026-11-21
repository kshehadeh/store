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

`store` runs on macOS and Linux (amd64 and arm64). It needs the
[1Password CLI](https://developer.1password.com/docs/cli/get-started/) (`op`).

### Homebrew (recommended)

```sh
brew install --cask kshehadeh/tap/store
brew install --cask 1password-cli   # if you don't already have op
store --version
```

Update or remove it later with:

```sh
brew upgrade --cask kshehadeh/tap/store
brew uninstall --cask kshehadeh/tap/store
```

The cask comes from [kshehadeh/homebrew-tap](https://github.com/kshehadeh/homebrew-tap).
On macOS it installs the release binary signed with a Developer ID and
notarized by Apple, so Gatekeeper allows it without extra steps.

If `store --version` doesn't show the release you installed, an older `store`
alias or binary is probably taking precedence. Check with `type store`.

### Download a release

Each [GitHub release](https://github.com/kshehadeh/store/releases) has a
`.tar.gz` archive per platform and a `checksums.txt` file. For example, on an
Apple Silicon Mac:

```sh
version=0.1.1   # the release you want, without the leading "v"
curl -LO "https://github.com/kshehadeh/store/releases/download/v$version/store_${version}_darwin_arm64.tar.gz"
curl -LO "https://github.com/kshehadeh/store/releases/download/v$version/checksums.txt"
shasum -a 256 -c --ignore-missing checksums.txt
tar xzf "store_${version}_darwin_arm64.tar.gz" store
install -m 0755 store ~/.local/bin/store   # any directory on your PATH
```

Use `darwin_amd64` for Intel Macs, or `linux_amd64`/`linux_arm64` on Linux
(where `sha256sum -c --ignore-missing checksums.txt` also works).

### Build from source

With Go 1.26 or newer:

```sh
go install github.com/kshehadeh/store/cmd/store@latest
```

This installs to `$(go env GOPATH)/bin`, which must be on your `PATH`. To work
on `store` itself, see [Development](#development).

Next, [set up each computer](#set-up-each-computer).

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
Taskfile.yml                Development and release commands
.goreleaser.yml             Release binaries, signing, archives, checksums, cask
scripts/                    Release helper and isolated Git tests
```

Install the development tools once (or use `brew install go-task goreleaser`):

```sh
go install github.com/go-task/task/v3/cmd/task@v3.54.0
go install github.com/goreleaser/goreleaser/v2@v2.20.3
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`. Go may download a newer
toolchain to build these tools; this does not change store's Go requirement.

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

Add that line to your shell configuration (for example, `~/.zshrc`) to keep
it across sessions. `task build` does not need GoReleaser; release checks and
snapshots do.

```sh
task                 # list commands
task fmt
task check           # formatting, modules, race tests, vet, build
task run -- pull
./dist/store --version
```

### Running from a checkout

`go run` compiles into Go's build cache and runs the CLI in one step. Put
`store`'s own arguments after the package path:

```sh
go run ./cmd/store status
go run ./cmd/store pull root-env --to ~/.env
```

When `go run` exits with an error, Go prints an extra `exit status 1` line.
To avoid that in daily use, build a binary and point an alias at it (replace
the path with your checkout):

```sh
go -C ~/src/store build -o dist/store ./cmd/store
alias store="$HOME/src/store/dist/store"
```

The binary runs in your current directory, so relative paths work normally.
Rebuild after changing the source. Remove the alias (`unalias store`) to go
back to an installed release.

CI runs formatting checks, module verification, race-enabled tests, vet,
and a build on macOS and Linux. Its tests use only the simulated vault.

The end-to-end tests in `cmd/store` build the real `store` binary and run it
against `internal/testsupport/fakeop`, a stand-in for `op` backed by a JSON
file. They simulate two computers with separate configurations sharing one
vault. They never call the real `op` or touch your vault.

### Releases

Git tags are the version source; there is no version file to edit or version
commit to create. Releasing requires GitHub write access, authenticated Git,
and a configured Git identity for annotated tags.

1. Merge the changes into `main` and confirm CI passes.
2. Update your checkout and check that it has no uncommitted or untracked
   changes:

   ```sh
   git switch main
   git pull --ff-only origin main
   git status --short
   ```

   The last command should print nothing.
3. Preview the version, then run the command for the desired bump:

   ```sh
   task release:patch -- --dry-run  # validate and preview; no tag or push
   task release:patch              # v0.1.0 initially, then v0.1.1, etc.
   task release:minor              # v0.1.1 -> v0.2.0
   task release:major              # v0.2.0 -> v1.0.0
   ```

   Replace `patch` with `minor` or `major` in the preview when needed.
4. Watch the [Release workflow](https://github.com/kshehadeh/store/actions/workflows/release.yml).
   A successful tag push starts the workflow; it does not mean publishing
   has finished.
5. Check [GitHub Releases](https://github.com/kshehadeh/store/releases) for the
   new version. Verify the downloaded archive against `checksums.txt` and
   run its binary with `store --version` to confirm the expected tag.

The first release is always `v0.1.0`. Later commands increment the highest
stable semantic-version tag, ignoring prerelease tags. They require a clean
`main` branch matching `origin/main`, fetch tags, run `task check` and
`task release:check`, then create an annotated tag and push only that tag.
They do not commit files, push uncommitted changes, or force-push anything.
If a tag push fails, the local tag remains so you can inspect and retry it.
If the workflow fails after the tag is pushed, inspect its logs. Transient
failures can be retried in GitHub Actions; fixes to the code or workflow
should use a new version rather than moving an existing release tag.

Pushing a `vMAJOR.MINOR.PATCH` tag triggers the release workflow. It validates
the tagged commit, then GoReleaser signs and notarizes the macOS binaries and
publishes a GitHub release with generated notes, macOS/Linux binaries for amd64
and arm64, `.tar.gz` archives, and `checksums.txt`. It then commits an updated
`Casks/store.rb` to [kshehadeh/homebrew-tap](https://github.com/kshehadeh/homebrew-tap).

The workflow refuses to publish unless these repository secrets exist:

| Secret | Contents |
|---|---|
| `MACOS_SIGN_P12` | Base64 of the exported "Developer ID Application" certificate (`.p12`) |
| `MACOS_SIGN_PASSWORD` | Password for that `.p12` file |
| `MACOS_NOTARY_KEY` | Base64 of the App Store Connect API key (`.p8`) |
| `MACOS_NOTARY_KEY_ID` | That API key's ID |
| `MACOS_NOTARY_ISSUER_ID` | The App Store Connect issuer ID |
| `HOMEBREW_TAP_DEPLOY_KEY` | Private SSH key for the tap's write-enabled deploy key |

Set file-based secrets without writing the encoded value anywhere:

```sh
base64 -i Certificates.p12 | gh secret set MACOS_SIGN_P12 --repo kshehadeh/store
base64 -i AuthKey_XXXXXXXXXX.p8 | gh secret set MACOS_NOTARY_KEY --repo kshehadeh/store
gh secret set MACOS_SIGN_PASSWORD --repo kshehadeh/store   # prompts for the value
```

Unsigned macOS binaries installed through the cask are quarantined and blocked
by Gatekeeper, so do not publish without signing. GitHub's built-in token
publishes the release itself; it cannot write to the tap, which is why the
deploy key exists.

To build the same packages locally without tagging or publishing:

```sh
task release:snapshot
```

Snapshot artifacts go to the ignored `dist/` directory. Release binaries
report their tag with `store --version`; `go install ...@VERSION` also
reports the module version.

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

## Contributing

Fork the repository, or clone it directly if you have write access:

```sh
git clone https://github.com/kshehadeh/store.git
cd store
git switch -c feat/describe-your-change
```

Install the tools listed under [Development](#development), then:

1. Keep changes focused. Keep `cmd/store` thin, command wiring in
   `internal/cli`, application behavior in `internal/app`, and vault-specific
   behavior in the provider adapter.
2. Add regression tests alongside the affected package. Extend the fake
   `op` and end-to-end tests when changing command behavior. Preserve exact
   pulled output on stdout, prompts on stderr, explicit field selection,
   and the refusal to overwrite existing destinations.
3. Update the README when commands, flags, or workflows change.
4. Run the same checks used by CI:

   ```sh
   task fmt
   task check release:check
   ```

   For packaging changes, also run `task release:snapshot`. Snapshots do not
   create tags or publish releases.
5. Commit only the intended source, tests, and documentation. Use a
   descriptive conventional commit message, such as `fix: preserve empty
   folders on restore` or `docs: clarify vault setup`. Do not commit vault
   configuration, credentials, real payloads, `dist/`, or `.task/`.
6. Push your branch and open a [pull request](https://github.com/kshehadeh/store/pulls)
   against `main`. Explain what changed, why, and which checks you ran.

Automated tests use simulated vaults and isolated local Git repositories.
They must not touch a real 1Password vault or publish a real release. Use
only a disposable vault for the optional manual smoke test above.

For a bug report, include the command, expected and observed behavior,
`store --version`, and your OS in an [issue](https://github.com/kshehadeh/store/issues).
Remove secret values and identifying vault or account details from logs
before sharing them.

## Adding a provider

Implement `provider.Provider` (`internal/provider`) and a `provider.Definition`
with `Setup`, `Open`, and `Describe`, then add it to
`internal/providers/providers.go`. The adapter maps the vault's native items,
tags, and IDs onto store's model; tag semantics, selection, ZIP handling, and
safe restoration stay in the core.
