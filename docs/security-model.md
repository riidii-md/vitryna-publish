# Security model

Vitryna publishes selected **content**, not private Git ancestry. Its safety
depends on reviewed visibility policy, trusted source controls, a dedicated App,
job isolation, and protected public history. Local mocks test implementation
ordering, not GitHub's enforcement; [hosted acceptance](hosted-acceptance.md) is
still pending.

## Trust boundaries

| Job | Inputs and credentials | Permitted work |
| --- | --- | --- |
| invalidate | Protected control snapshot; source token; App root key | Check freshness/ownership; disarm then close old candidate; revoke target token |
| seal | Committed source; read-only source checkout token | Classify/export; seal tree and private receipt; upload original bundle |
| verify | Original artifact identity/digest; source artifact-read token only in download step | Independently check bundle; run project code on disposable tree; check it again; read-only public comparison |
| reconcile | Original seal outputs, not project output; source token; App root key | Validate artifact before write-token acquisition; recheck freshness; leased promotion update and PR lifecycle; revoke target token |

No App key or target token is passed to project verification. Credentialed jobs
download a public checksum-pinned binary and never build or execute source project
code. Checkout credentials are not persisted. Source Git subprocesses receive an
allowlisted environment without App/source tokens; no replacements or lazy fetch
are allowed. Missing objects must be prefetched outside Vitryna.

This is not a sandbox for arbitrary malicious code on the same host. A compromised
runner, source administrator, modified credentialed workflow, or substituted
trusted release can defeat these boundaries. Use isolated hosted jobs and review
controls. The source checkout and Actions runtime still have platform capabilities;
"no target credentials" does not mean "no network access".

## Artifact integrity and provenance

The seal contains only canonical `tree.tar` and `receipt.json`. Receipt provenance
includes private commit/config/policy digests; never attach it to a public PR.
The publisher binds the artifact to source repository ID, workflow run, artifact
ID, immutable source commit, expiry, and provider digest. It hashes the bounded
raw ZIP **before extraction**, then verifies exact entries, bounded canonical tar,
file inventory, modes, bytes, receipt encoding, and tree digest. Redirected storage
downloads never receive the source Authorization header.

Archive links, traversal, duplicate/missing/extra entries, special modes, unknown
receipt fields, and mismatched digests fail. Limits are in
[configuration](configuration.md). Verification output cannot replace the original
seal; reconciliation downloads it again. A failed verification prevents reconcile.
The generated upload is immutable, non-overwriting, and retained for seven days.

Before project commands, the verify job captures the original canonical receipt's
SHA-256 in a runner-owned step output. Postverification and preview require that
digest, hashing and decoding the same bounded receipt bytes. Rewriting both the
copy and its adjacent receipt cannot redefine the verification baseline. An
empty or malformed explicitly supplied binding is refused, not treated as opt-out.

Artifact staging and preview-download cleanup retain directory identity from
creation and refuse a changed path. Preview source/tool identity is checked
before extracted output is exposed. Ownership checks reduce accidental concurrent
replacement loss; they are not atomic filesystem protection against an actively
hostile same-host process. A cleanup failure is a failure, not permission to
recursively remove an unfamiliar replacement.

## Target ownership and history

The target Git repository contains only public base/promotion objects. Raw blob
and index construction ignores attributes, filters, hooks, and line-ending
conversion. Every new promotion commit has exactly one public-base parent and
the App bot identity. Public commit/PR messages are fixed and contain no private
source SHA, config digest, paths, receipt, or run URL.

Ownership requires the configured repository/refs, App actor, fixed commit and PR
markers, and matching observed head. A branch name or body marker alone is not
ownership. Unowned or ambiguous state fails untouched. Only the promotion ref is
updated/deleted, with an explicit expected-head lease. Public base/default refs
are reserved; metadata is re-read before mutation. Competing heads fail rather
than being force-overwritten. Public base movement is rebuilt only a bounded
number of times. Entire-tree publication removes public-only files by design.

App topology is checked before scoped write-token minting. Runtime target tokens
use only necessary permissions. Tokens are kept out of argv and repository
configuration files; Git receives an ephemeral HTTPS header through its process
environment. That environment remains sensitive to privileged same-host
inspection. Tokens are revoked on normal/error completion; forced termination
can prevent cleanup.

## Freshness and unavoidable races

The trusted source tip is checked repeatedly, including immediately before
publication/PR changes/native auto-merge. Owned old candidates are invalidated
before new export/tests. Uncertain writes are observed before bounded retries;
leases are not blindly retried over competing updates.

These checks do not form a transaction with GitHub. A public candidate may merge
before a later private push's invalidation succeeds. A target administrator can
change the default branch between metadata checks and mutation. A PR may merge
while Vitryna is disarming/closing it. Reported merged-state warnings require
human inspection; Vitryna never rewrites the public base to hide a race. Use manual
merge when these asynchronous windows are unacceptable.

## Policy is the primary confidentiality control

Public subtrees include future descendants. Forbidden strings are optional
literal guards, not comprehensive secret scanning. There are no hidden project
exclusions, globs, or feature stripping. A misclassified private file can be
correctly exported and disclosed. Review policy changes and inspect the exported
edition before enabling publication. Generated setup is convenience, not approval.

Go's Unicode normalization dependency is currently pinned to preserve deterministic
path semantics. `golang.org/x/text v0.32.0` has the invalid-UTF-8 normalization
advisory [GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970); all normalization
entry points first reject invalid UTF-8. A normalization-data upgrade remains a
reviewed compatibility change, not an unexamined dependency bump. This guard is
tested; do not remove it when refactoring.

For confidentiality incidents, use [incident response](incident-response.md).
