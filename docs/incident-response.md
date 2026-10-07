# Incident response

Do not retry blindly or rewrite history automatically. First establish whether
the issue is a publication failure, functional public regression, or disclosure.
Keep evidence private and redact credentials/signed URLs from logs.

## Failed invalidation or uncertain control write

1. Pause/disable the source publication workflow. Inspect its safe candidate link
   and the current public promotion ref/PR, including auto-merge state and actor.
2. Disarm and close the **exact owned** candidate through GitHub. If it already
   merged, treat that as public state; do not pretend invalidation prevented it.
3. Check permissions, source-tip freshness, installation topology, provider status,
   and whether another publisher touched the promotion ref. Preserve unrelated PRs.
4. If token revocation failed or exposure is suspected, revoke/suspend the App or
   retire its key. Expiry is not an adequate response to an active credential leak.
5. Resume only after current owned state is understood and the current trusted tip
   can run all gates. Never force over a competing head or repurpose the branch.

Cancellation can prevent cleanup. Native auto-merge may have been enabled despite
a lost response; inspect rather than assuming it stayed off.

## Bad public content without confidential disclosure

Before merge, close the owned PR and disable the new workflow. For Tatami rollout,
restore the retained legacy workflow only after ensuring there is one active
publisher. After merge, make an ordinary public revert and correct the private
source/policy. Run verification and review a new promotion. Keep public history
auditable; do not use a forced base reset as normal rollback.

## Confidential content or credential disclosure

1. Suspend publication and any armed candidate immediately. Rotate/revoke exposed
   credentials, including application credentials found in published files.
2. Preserve a minimal private incident record: affected public paths/commits/PRs,
   exposure window, artifact/run IDs, access scope, and actions taken. Do not copy
   confidential file bodies into a new broadly accessible record.
3. Inspect owned promotion refs, public commits/PR diffs, workflow artifacts/logs,
   releases, caches, and known forks/clones. Closing a PR or deleting a ref does
   **not** erase disclosed objects or external copies.
4. Coordinate sensitive-data removal with repository owners and GitHub support as
   appropriate. Follow [GitHub's removal guidance](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository).
   Destructive history changes require a separately reviewed incident action; this
   tool does not automate them. External clones/forks may remain beyond your control.
5. Correct the visibility policy, add a synthetic regression test, rerun parity
   and acceptance where relevant, and obtain review before resuming publication.

Forbidden markers are not a secret scanner. Include the policy/process failure
in the follow-up, not only an extra literal string. Maintain source failure alerts
and a named operator responsible for owned-candidate inspection.
