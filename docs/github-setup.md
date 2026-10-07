# GitHub setup

The adapter is experimental until [hosted acceptance](hosted-acceptance.md)
passes for a released candidate. Examples below use fictional repositories.

## Prerequisites

Use GitHub.com, Git 2.45+, and Linux/macOS. The generated workflow uses hosted
Ubuntu 24.04 runners and fully SHA-pinned Node 24 Actions. Self-hosted runners
must support these Actions and preserve the job-isolation assumptions; reusable
dirty runners are not a verified deployment target.

Create `example/widget.dev` as the development source and `example/widget` as the
public target. Initialize the target's `main` yourself. Reserve `vitryna/promote`
for this publisher only. It cannot be the configured base or current default.
Publication replaces the entire public tree, not a subset layered over the target.

Protect the trusted source branch. Require review of the configuration and
workflow, for example with CODEOWNERS. Source administrators and reviewed control
changes are trusted: someone able to alter the credentialed workflow can change
its security boundaries. Do not grant the App branch-protection bypass rights.

## Dedicated App and permissions

Create a dedicated GitHub App with exactly one installation, using selected
repositories with exactly the intended public target. Do not reuse a multi-repo
App. Vitryna checks the actual installation inventory with a metadata-only token
before minting a repository-scoped write token; selecting one repository in a
write-token request alone is not proof of the App's wider scope.

| Permission | When required |
| --- | --- |
| Metadata: read | Every mode; repository identity and effective rules |
| Contents: write | Invalidation/reconciliation control jobs |
| Pull requests: write | Managed PR lifecycle and native auto-merge |
| Workflows: write | Desired **or existing** public tree contains workflow files, including their deletion |
| Administration: read | Only explicit auto mode with `protection_inspection: classic` |

Manual mode never requests Administration read. Effective-ruleset mode uses
[branch rules with Metadata read](https://docs.github.com/en/rest/repos/rules#get-rules-for-a-branch);
classic mode additionally uses [classic protection with Administration read](https://docs.github.com/en/rest/branches/branch-protection#get-branch-protection).
Grant no unrelated write or administration permissions. A rejected permission or
unavailable policy query fails closed; do not solve it by granting bypass rights.

Set source Actions variable `VITRYNA_APP_CLIENT_ID` and source Actions secret
`VITRYNA_APP_PRIVATE_KEY` (the App's PEM RSA key). Do not paste keys into CLI
arguments, config, examples, logs, or receipts. The generated workflow maps its
source `GITHUB_TOKEN` to `VITRYNA_SOURCE_TOKEN` only in the required CLI steps.
Installation tokens are short-lived and revoked after each control invocation;
see [GitHub's installation token API](https://docs.github.com/en/rest/apps/installations).

## Generate and review

Run the README's `init github` command with a released binary. Add `--public`
once per exact file or subtree, and a real `--verify-command`. All other currently
tracked paths become exact private rules. The three generated files are private
controls; the setup guide uses an explicit private rule. Do not publish the App
configuration or private guide.

`--offline --pins /path/to/vitryna-pins.json` skips target lookup, not runtime
checks; the manifest version must match the released binary. Online setup can
report a missing target base, but never initializes it. If files conflict, init
leaves them unchanged. Edit reviewed setup deliberately; rerunning init is not an
upgrade/overwrite command.

Review all three files, commit them with the selected source files, and run:

```sh
vitryna check --repository /path/to/source --format json
vitryna explain --repository /path/to/source src/main.go
vitryna export --repository /path/to/source \
  --output /path/to/fresh-preview --receipt /path/to/fresh-receipt.json
```

The workflow is version/checksum pinned. To upgrade, review a new release's pins
and regenerate in a disposable clean checkout or update the reviewed workflow;
never replace a version's assets in place. Changes to rendered source, target,
base, promotion, or trusted-source identities must match committed config.

## Verification and preview

Project commands run in a disposable public-tree copy, never in the credentialed
control jobs. Use build/cache output outside that tree; modifications, missing or
added files, and mode changes fail post-verification. Commands that need excluded
private files are not valid tests of the public edition. The seal was created in
a separate job **before** your commands and is re-downloaded independently for
publication. Artifacts contain private provenance: keep them private.

Keep the generated original-receipt step output and `--receipt-sha256` arguments
in postverification and preview. They bind checks to the downloaded seal even if
project commands rewrite the adjacent receipt. Verification commands accept LF
and CRLF (normalized to LF); other YAML line separators are refused during setup.

Private PRs and dispatch with `publish: false` preview only. Trusted-branch pushes
publish; a dispatch with `publish: true` must still match the current trusted tip.
Pair-specific concurrency has `cancel-in-progress: false`; queue order does not
prove freshness. A preview's `hasDiff` is informational: it never skips trusted
no-diff cleanup.

Public CI must test the proposed public tree and block the public base as your
policy requires. A source verification pass is not a substitute for public CI.
The unauthenticated public preview uses public GitHub API reads and is subject
to public API rate limits. Vitryna does not retry by escalating permissions.

## Optional native auto-merge

Manual is the default and works without protection discovery. To opt in, set:

```yaml
merge:
  mode: auto
  method: squash
  protection_inspection: effective_rulesets
```

Enable repository auto-merge and the requested merge method. Require at least one
positive blocking gate: a named required check, a positive required-review count,
or an applicable merge queue. Every applicable review gate must invalidate stale
approval or require approval after the last push. Reviewer routing or a zero
approval count alone does not qualify. Queue method must match the explicit
method. Classic mode also checks effective rules in addition to classic settings.

Vitryna requests native auto-merge with the expected public head; it never invokes
an immediate merge endpoint. Missing gates, inaccessible policy, stale source,
unsupported methods, or denied native requests leave the candidate open for
manual review and fail visibly. An ambiguous post-write response can mean the
request succeeded: inspect the exact candidate before retrying.

An armed PR can merge before a later private push's asynchronous invalidation
succeeds. There is **no atomic latest-private-source merge guarantee**. Start
manual, prove the sandbox cases, then decide whether this race is acceptable.
Never repurpose the promotion branch while a run is active; last-minute metadata
checks cannot lock out a concurrent administrator.

## Failure alerts and rotation

Enable Actions failure notifications for source maintainers. The early invalidation
job disarms and closes the older owned candidate before export/project tests. If
that job fails, stop publication and inspect the candidate link in the job summary:
an older PR may still be armed. Follow the [incident runbook](incident-response.md).

Rotate the App key by adding a new key, updating the protected source secret,
verifying a sandbox run, and retiring the old key. A canceled or killed process
may not complete revocation; suspend/revoke the App when exposure is suspected.
Vitryna does not create Apps, configure branch rules, or repair credentials for you.
