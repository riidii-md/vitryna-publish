# Tatami migration — preparation only

Tatami's embedded exporter remains authoritative. This framework does not change
Tatami, enable its new workflow, or remove legacy code. Migrate only after a
reviewed Vitryna release and hosted sandbox acceptance, under a separately approved
private-repository task. Do not combine extraction with visibility changes.

## Mechanical conversion in the private repository

Use a temporary, repository-local converter. Read `.public-export.json` from the
chosen immutable commit, not dirty worktree content. Require `schemaVersion: 1`
and the expected fields; reject unknown/duplicate fields and trailing JSON.

| Legacy JSON | Vitryna YAML |
| --- | --- |
| `schemaVersion: 1` | `version: 1` |
| `publicPaths` | `export.public` |
| `privatePaths` | `export.private` |
| `forbiddenContent` | `export.forbidden_content` |

Preserve exact/subtree rules and marker strings. Retain explicit `docs/private`;
it is no longer a hidden provider-wide rule. Keep the old manifest/exporter/
workflow private with explicit rules, and add the generated setup guide's exact
private rule. Vitryna's config and named publication workflow are automatic
private controls. Resolve redundant rules, new portability failures, or private
special entries deliberately; do not silently alter visibility to make conversion
pass. Public globs/section stripping are not available.

The converter should emit YAML via a real encoder, feed it through `config.Decode`,
and write a **fresh** candidate file for review. Do not overwrite the existing
manifest or checkout. Keep converter output/receipts/logs private and temporary.
No Tatami-specific converter is shipped as a public CLI feature.

## Same-commit parity

Commit both reviewed controls in a private migration branch while legacy remains
active. Resolve one full SHA containing both policies, populate all objects, and
invoke both exporters against that **same SHA** into different fresh destinations
outside the source. For example, using separately built/verified binaries:

```sh
tatami-public-export --repository /path/to/private/source --ref "$SOURCE_SHA" \
  --manifest .public-export.json --output /path/to/fresh-legacy-tree \
  --receipt /path/to/fresh-legacy-receipt.json
vitryna export --repository /path/to/private/source --ref "$SOURCE_SHA" \
  --output /path/to/fresh-vitryna-tree --receipt /path/to/fresh-vitryna-receipt.json
```

Compare the complete sorted path inventory, each file's raw bytes, and Git modes
`100644`/`100755`. Normalize both receipts to sorted `(path, mode, size, sha256)`
records and require exact equality, plus both source SHAs equal the selected SHA.
Receipts themselves differ intentionally in schema/control/policy/tool fields;
do not equate full receipt digests. Directory permissions/timestamps are not
source Git-file parity. Any mismatch stops rollout. Preserve only safe summary
evidence publicly; real records, paths, and source bodies remain private.

The public [synthetic fixture](../testdata/parity/README.md) provides a permanent
literal normalized oracle. The integration test proves committed inputs, public
bytes/CRLF/executable modes, private exclusion, and sealed-copy isolation. To also
run the actual legacy binary on that **invented** fixture:

```sh
VITRYNA_LEGACY_EXPORT_BINARY=/absolute/path/to/tatami-public-export \
go test ./test/integration -run '^TestSyntheticTatamiNormalizedParity$' -count=1 -v
```

This is not evidence of actual private Tatami parity; that remains a gated later
step. Never copy private Tatami fixture bodies into this public repository.

## Shadow, cutover, and rollback

1. Add a secretless, non-publishing shadow export/verification workflow in the
   private repo; legacy publication remains authoritative. Compare a representative
   immutable commit series without changing public visibility.
2. Verify the dedicated target-only App, target protections/public CI, runtime
   pins, and failure alerts. Keep manual mode for initial rollout.
3. Disable legacy publication before enabling Vitryna publication. Use a distinct
   reserved promotion branch with exact ownership; never run competing publishers.
4. Review one manual promotion, run public CI on its exact public tree, and merge
   through normal public review. Retain the old exporter through this full cycle.
5. Remove the temporary converter/legacy exporter in a separate reviewed change.
   Optional auto-merge is a later repository policy decision.

Before merge, rollback is disabling Vitryna, disarming/closing its owned candidate,
and restoring retained legacy workflow. After a functional bad merge, use a public
revert/private correction. A confidentiality disclosure requires the
[incident runbook](incident-response.md), not routine rollback or an automatic
history rewrite.
