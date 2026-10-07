# Release preparation and installation

There is no first released version yet. Local packaging is implemented; remote
bootstrap, tags, release publication, and sandbox use require separate approval.
Never claim a release exists because a local fixture built successfully.

## Build contract

Requires Go 1.25.1+; CI/release workflows pin Go 1.27.1. Build from the reviewed
public revision, with a fresh destination:

```sh
go run ./cmd/vitryna-release --version v0.1.0-rc.1 --output dist/v0.1.0-rc.1
```

The flat asset inventory is exactly:

```text
vitryna-linux-amd64
vitryna-linux-arm64
vitryna-darwin-amd64
vitryna-darwin-arm64
vitryna-pins.json
checksums.txt
```

CGO is disabled. Each binary uses `-trimpath`, disabled VCS embedding, and an
injected version. The manifest pins the four actual binary SHA-256 digests and
`github-zip-bundle-v1`; checksums also cover the manifest. Build twice with the
same SDK/input and compare manifests. CI checks tests, vet, formatting, race,
generated-workflow actionlint, version, checksums, and exact six-file inventory.

Checksums detect mismatch against reviewed pins; they are not signatures or proof
against a compromised release origin. Preserve reviewed release assets unchanged.

Every standalone executable embeds [complete legal material](../internal/cli/notices.txt),
available through `vitryna licenses`: Vitryna MIT, retained Tatami MIT attribution,
Go/x-text BSD and patent grant,
exact YAML LICENSE/NOTICE, and the full [Apache 2.0 license](https://www.apache.org/licenses/LICENSE-2.0.txt).
No companion download is necessary. Review these texts with SDK/dependency changes;
do not replace full terms with links or just the Apache boilerplate. Local and
native-platform CI tests check the legal command and built-binary embedding.

## Development fixture

For local onboarding tests before the first public release:

```sh
go run ./cmd/vitryna-release --version v0.1.0-fixture --output dist/v0.1.0-fixture
./dist/v0.1.0-fixture/vitryna-darwin-arm64 version
./dist/v0.1.0-fixture/vitryna-darwin-arm64 init github \
  --repository /path/to/committed-synthetic-source \
  --source-repository example/widget.dev --target example/widget \
  --public README.md --public src --verify-command 'go test ./...' \
  --offline --pins dist/v0.1.0-fixture/vitryna-pins.json
```

Choose your actual host platform asset. These generated workflows contain a
nonexistent fixture release URL and **must not be enabled** on GitHub. They test
local generation/classification only. Use a separately authorized candidate
prerelease for hosted acceptance.

## Reviewed publication

The empty remote first needs an explicitly approved initial public base commit;
a normal PR cannot target nonexistent history. After exact-snapshot approval,
separately authorize that bootstrap/publication operation, protect the public
branch and release tags, and configure the `release` environment with required
human approval. No workflow can configure that environment for you.

The prepared release workflow is manual-dispatch only. Supply an existing reviewed
version tag; it checks that the resolved tag commit equals the checked-out commit,
tests/builds, and uploads the six assets. Draft and prerelease default to true.
Do not dispatch until repository/bootstrap/tag/release authority is granted.
Never silently replace existing version assets. Hosted acceptance must pass
before changing the adapter's production-readiness status.

## Install a released binary

Once a release exists, download the matching platform asset, manifest, and
checksums from that version's release page. Verify the chosen asset SHA against
reviewed pins/checksums before making it executable. For example on Linux:

```sh
sha256sum vitryna-linux-amd64
chmod +x vitryna-linux-amd64
./vitryna-linux-amd64 version
./vitryna-linux-amd64 help
./vitryna-linux-amd64 licenses
```

On macOS use `shasum -a 256`. Move it to a chosen user-owned PATH directory only
after verification; confirm `command -v vitryna`, `vitryna version`, and
`vitryna help`. Windows is not supported in v1. Generated GitHub workflows fetch
the Linux amd64 asset from the exact release and verify its embedded expected SHA
before checkout or credentials are supplied to control steps.
