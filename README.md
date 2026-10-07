# Vitryna Publish

Repository-owned publication rules for a separate public repository.

Keep developing in one repository. Publish a deliberately selected edition to
another, without copying private Git history or moving your development workflow.

Your source repository owns `.vitryna.yml`: exact files or whole subtrees are
public; everything else must be explicitly private. Vitryna exports committed
bytes, verifies a sealed snapshot, and proposes the complete public tree through
a managed pull request. Manual merge is the default. Repository owners can opt
into GitHub-native auto-merge behind public checks or fresh reviews.

**Status:** local export, GitHub adapter, setup generation, and release packaging
are implemented. Hosted sandbox acceptance is pending. No first release is
available yet; do not treat the GitHub adapter as production-ready.

## Development

Requires Go 1.25.1 or newer. The export engine requires Git 2.45 or newer
with `--no-lazy-fetch` support; it reads local committed objects only.

```sh
make test
make vet
make build
./bin/vitryna help
./bin/vitryna version
```

No global installation is required. Invoke this exact binary, or place a reviewed
release binary on your own PATH and confirm `command -v vitryna` and
`vitryna version`. [Release preparation](docs/releasing.md) explains the four
Linux/macOS assets and checksum-pinned workflow bootstrap.

## Set up another repository

The following is the intended released-binary workflow. Until the first release,
use the [local fixture instructions](docs/releasing.md#development-fixture).
Start from a committed source repository and an existing public target with an
initialized base branch:

```sh
vitryna init github --repository /path/to/source \
  --source-repository example/widget.dev --target example/widget \
  --public README.md --public src --public go.mod --public go.sum \
  --verify-command 'go test ./...'
```

This creates `.vitryna.yml`, `.github/workflows/vitryna-publish.yml`, and a private
`.vitryna-setup.md` checklist. Review and commit them yourself. Currently tracked
paths outside your selections become exact private rules. Existing differing
setup files are never overwritten. With no verification command, the generated
job intentionally fails until you supply one.

The example publishes future descendants of `src`. New paths elsewhere fail
until you classify them. There are no hidden project exclusions or inferred
visibility defaults. The publisher replaces the **whole target tree**: target-only
files and public hand edits are not preserved automatically. Keep required public
CI and documentation in your source selection.

Follow [GitHub setup](docs/github-setup.md) for the dedicated App, source and public
protections, credentials, preview-only runs, and optional native auto-merge. Init
does not create repositories, initialize history, install an App, or change rules.

## Local export

Commit a `.vitryna.yml` that classifies every tracked path, then run:

```sh
./bin/vitryna check --repository /path/to/source --ref HEAD
./bin/vitryna explain --repository /path/to/source README.md
./bin/vitryna export --repository /path/to/source --ref HEAD \
  --output /path/to/public-tree --receipt /path/to/receipt.json
```

Output and receipt must be outside the source worktree; the receipt must also
be outside the output. Existing receipts and nonempty output are refused.
Dirty files never override committed configuration or content. Source object
reads disable Git replacement refs and lazy fetching. Prefetch missing objects
outside Vitryna before retrying.

See [configuration](docs/configuration.md) for rules, defaults, limits, and the
receipt contract. Reusable Go APIs live in `config` and `export`; their examples
run as tests. GitHub plumbing is internal, not a stable library API.

## Operator documentation

- [GitHub setup](docs/github-setup.md) — onboard a source/target pair.
- [Security model](docs/security-model.md) — credentials, artifacts, ownership, and races.
- [Troubleshooting](docs/troubleshooting.md) and [incident response](docs/incident-response.md).
- [Hosted acceptance](docs/hosted-acceptance.md) — the pending sandbox gate.
- [Tatami migration](docs/migration-tatami.md) — parity first, cutover later.
- [Roadmap](docs/roadmap.md) — contributions, profiles, releases, richer selection.

MIT licensed. Every standalone binary includes complete applicable license and
dependency notices; run `vitryna licenses`. The reviewed source copy is
[binary notices](internal/cli/notices.txt).

The committed-tree selection and export algorithms adapt Tatami's MIT-licensed
exporter by Oleksandr Besan (2024); the original attribution and full MIT text
are retained in the source license and embedded notices.
