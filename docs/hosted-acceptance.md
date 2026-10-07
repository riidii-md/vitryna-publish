# Hosted acceptance — pending

Local tests are not proof of hosted artifact semantics, App enforcement, secret
isolation, merge queues, or native auto-merge. P17 must pass on a candidate release
before calling the GitHub adapter production-ready. Nothing in this document or
the opt-in reader creates GitHub resources or authorizes publication.

## Explicitly authorized sandbox

Obtain separate approval for a candidate prerelease, a dedicated **private** source
and **public** target, target base initialization, App creation/installation,
credentials, protections, and test workflow/PR mutations. Use invented content,
never real Tatami files or production credentials. Record the exact allowed repos,
App, refs, actions, operator, release version, and cleanup authority privately.

Install a dedicated App on exactly the public sandbox target. Configure reviewed
source controls, native auto-merge settings when needed, and public checks/reviews
with no App bypass. Generate setup using the actual candidate's pins. Do not run
a fixture-version workflow against a nonexistent release.

## Scenario evidence

Maintain a private record per scenario; a public summary may state pass/fail and
synthetic public references, not raw private receipts or tokens.

| Scenario | Required observed evidence |
| --- | --- |
| Manual changed tree | Four-job ordering; original artifact ID/run/digest; App-owned open unarmed PR; exact public bytes/modes; one public parent |
| Same tree / reverted desired tree | Old owned candidate closed/disarmed; leased promotion deletion only; base/default unchanged; no-diff still reconciled |
| Failed source verification | Old candidate invalidated first; reconcile skipped; public base unchanged |
| Preview PR / publish-false dispatch | Invalidate skipped; seal/verify run; no App credential job or target mutation |
| Stale/feature dispatch | Freshness failure before write token; no publication |
| Artifact tamper / wrong provenance | Download identity/digest rejection before reconciliation App write token |
| Verification mutation | Post-verify failure; original bundle immutable; reconcile skipped |
| Exact App scope | One selected installation/target inventory; scoped token repository, permissions, expiry, revocation; a wider App denied before write mint |
| Credential zones | Reviewed workflow and isolated job logs show no App/target credential supplied to project step; sentinel checks without printing credentials |
| Public CI / workflow removal | Public CI tests proposed tree; adding/replacing/deleting public workflows requires Workflows write; no source-token target write |
| Ruleset native auto | Positive check/review/queue gate; expected-head native request; real App actor/method state; no immediate merge/bypass |
| Classic native auto | Administration read only this mode; classic and effective rules evaluated; same native lifecycle |
| Approval freshness | New head dismisses/requires fresh approval as configured; unsafe review gates denied |
| Denied native request | Candidate stays open; visible error; no immediate-merge fallback |
| Competing head / moved base | Explicit lease conflict preserves competing ref; base changes rebuilt only within bound |
| Reserved/default destination | Safe sandbox-only test: promotion made default; mutation refused and reserved refs unchanged |
| Invalidation failure / already merged | Alert identifies exact candidate; operator can disarm/close; merged race reported, not hidden or base-rewritten |
| Forced cancellation / key rotation | Inspect token/candidate state, revoke or suspend as needed; rotate source secret and retire old key |

Do not expose real credentials to "prove" isolation. A secret sentinel should be
non-sensitive, and a project check should report only absence/presence. Review
the actual permission/expiry response privately without logging token values.
Some lifecycle/race cases require a human scheduler; do not claim they ran from
the existence of a mocked local test or one successful candidate read.

## Opt-in read-only candidate runner

`TestHostedCandidateReadOnly` independently downloads one selected original
artifact, validates raw ZIP/seal/receipt provenance, inspects an **open** owned
public PR/commit, checks its one expected public parent, and compares its entire
public tree with the sealed original. It never accepts an App key or target write
token and never creates, closes, merges, or pushes anything. Run it before the
candidate merges or its promotion ref is deleted.

Prepare a private JSON record with these fields and real sandbox values:

```json
{
  "source": "example/acceptance-source",
  "target": "example/acceptance-public",
  "version": "v0.1.0-rc.1",
  "sourceRepositoryId": 1,
  "runId": 2,
  "artifactId": 3,
  "botId": 4,
  "pullNumber": 5,
  "sourceCommit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "publicHead": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "publicParent": "cccccccccccccccccccccccccccccccccccccccc",
  "digest": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
  "base": "main",
  "promotion": "vitryna/promote",
  "expectedAuto": false
}
```

IDs and hashes above are placeholders, not runnable acceptance evidence. The
digest is the raw upload archive's lowercase SHA-256 without `sha256:`. Use the
public base at construction time as `publicParent`, not a later moving base tip.
Load a read-only source Contents/Actions token into `VITRYNA_SOURCE_TOKEN` through
your credential mechanism; do not paste it into command arguments or logs.

```sh
VITRYNA_HOSTED_ACCEPTANCE=read-only \
VITRYNA_HOSTED_RECORD=/path/to/private/candidate-record.json \
go test ./test/integration -run '^TestHostedCandidateReadOnly$' -count=1 -v
```

Default `go test ./...` skips this reader and makes no hosted calls. Its pass is
only candidate/artifact evidence, **not** completion of the scenario matrix.

## Private evidence template

For each case record: operator/authorization, date, candidate version and binary
pin, source run URL/commit, target PR/public SHA, artifact ID/digest, selected App
installation inventory, scoped permissions/expiry/revocation outcome, public
checks/reviews, before/after refs and candidate state, expected result, actual
result, and residual uncertainty. Exclude token values, keys, signed storage URLs,
and source bodies. Mark each unrun case **pending**, not passed by inference.

Cleanup requires separate scoped approval: close/disarm exactly owned candidates,
remove only owned disposable refs/artifacts, revoke sandbox credentials, and
retire the dedicated installation. Never delete a repository or rewrite history
because this checklist mentions cleanup.
