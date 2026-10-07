# Troubleshooting

Commands return 0 for success, 1 for failed execution/validation, and 2 for CLI
usage errors. `check`, `explain`, and `export` support `--format json`. Diagnostics
identify safe error codes/paths, not file contents or provider error bodies.
Use `vitryna help` or the command's `--help` for actual flags. Do not enable shell
tracing around credentials or paste raw private receipts into public issues.

| Symptom | Safe next action |
| --- | --- |
| Unclassified file / unmatched public root | Inspect the selected commit, classify every tracked path, and commit policy; dirty edits are not input |
| Overlap / redundant rules | Choose disjoint exact files or subtrees; there is no precedence or exclusion override |
| Nonportable path, symlink, gitlink, LFS pointer | Keep it private or change source layout deliberately; Vitryna never renames/materializes it |
| Missing object / unsupported Git | Install Git 2.45+; explicitly populate the selected objects outside Vitryna, then retry offline |
| Output or receipt conflict | Choose fresh paths outside source; inspect existing user files rather than deleting them blindly |
| Init conflict | Review existing config/workflow/guide; init never overwrites partial or changed setup |
| Development/offline pins required | Build a local fixture with matching version and supply `--pins`; released generation requires that release's manifest |
| Missing public base | Initialize target history yourself and rerun; this is not an exporter failure |
| Setup control identity mismatch | Review both committed config and rendered workflow identities; do not edit only one |
| Verify changes tree/inventory/mode | Put build/cache output outside the disposable tree and test only the public edition |
| Artifact identity/digest/expiry failure | Stop; rerun from current trusted tip to obtain a new seal; never bypass the digest or substitute project output |
| App scope invalid | Use a dedicated one-installation, one-target selected-repository App; do not widen permissions |
| Workflow update denied | Grant Workflows write only if existing or desired public workflows require it |
| Stale source | Rerun the current trusted tip; queued order is not proof of freshness |
| Unowned/ambiguous branch or PR | Inspect actor, markers, refs, head, and competing publisher; do not force over it |
| Lease conflict / repeated base movement | Inspect the competing update, then rerun from fresh source/base; never weaken leases |
| Auto denied / missing gate | Candidate remains open; configure a positive gate with fresh reviews, or merge manually after review |
| Public API limit/unavailable | Retry after provider recovery; unauthenticated preview/metadata reads can be rate limited |
| Invalidation/revocation error | Follow the incident runbook; an old candidate or token may still be active |

Use the exact candidate link in the control job summary for ownership/auto-merge
inspection. An ambiguous response is not proof that a write failed. GitHub's UI
and current refs are the source of operational truth after a timeout.

For a safe bug report include binary/Git/OS versions, command name, diagnostic
code, and a **synthetic** reproducer. Redact tokens, signed artifact URLs, private
repo identifiers/commits, root keys, and file bodies. Public PR metadata is safe
only if your own policy allows sharing it.
