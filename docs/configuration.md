# Configuration v1

Vitryna reads `.vitryna.yml` from the selected Git commit, not the worktree.
Commit the configuration with your source changes before checking or exporting.

```yaml
version: 1
export:
  public: [README.md, src]
  private: [docs/private, .gitignore]
  forbidden_content: [PRIVATE_ONLY]
```

Each rule selects exactly its path or every tracked descendant below it. Every
tracked path must be classified. Unclassified paths fail; each public rule must
match a tracked path. Unused private roots can protect future content. Public
subtrees intentionally cover new descendants: review that choice carefully.

Both public and private lists must be nonempty. Overlapping or redundant rules
are rejected rather than assigned precedence. `.vitryna.yml` and
`.github/workflows/vitryna-publish.yml` are automatic private controls. No public
rule may include them. Exact private entries for those controls are allowed.
There are no other automatic exclusions, including `.agent-runs` or `.github`.

Only one YAML document is accepted. Unknown fields, duplicate keys, aliases,
anchors, merge keys, custom tags, type coercion, and unsupported versions fail.
String sequences must contain actual YAML strings: quote numeric/bool-like values
if they are intended as strings. Configuration is limited to 1 MiB and depth 32.

## Public files

Public files must be regular Git blobs with mode `100644` or `100755`. Executable
modes are preserved. Symlinks and submodules may remain private, but cannot be
public. Git LFS pointers are rejected; Vitryna does not materialize them.

Public paths must be UTF-8 and NFC, with no absolute/traversal/backslash paths,
control characters, `.git` component, Windows reserved names or characters, or
trailing dots/spaces. Unicode case-fold collisions are rejected at every prefix,
including differently cased directory names. Paths are never silently renamed.

Forbidden markers are literal strings, checked across streaming boundaries.
Empty/duplicate markers fail. Diagnostics do not print marker values or file
bodies. This is defense in depth, not a secret scanner or substitute for policy
review. There are no globs, overlays, feature stripping, or transformations in v1.

## Fixed limits and receipts

V1 permits at most 64 MiB per public blob, 512 MiB public content in total,
100,000 public files, and 1,024 UTF-8 bytes per public path. Receipts are limited
to 16 MiB. All limits apply together; meeting one does not waive another.
The sealed-artifact and raw provider ZIP envelopes are each limited to 1 GiB.

Receipt JSON includes schema/tool versions, source commit, raw config and
canonical policy digests, effective limits, sorted file paths/modes/sizes/digests,
file count, total bytes, and tree digest. It has no timestamps or host paths.
The same committed input and tool version produce identical receipt bytes.
Source commit provenance belongs in local/private evidence, not public PR text.

Output is staged and verified before exposure. A receipt is written only after
the tree is in place, without overwriting an existing receipt. Consumers must
validate both tree and receipt; a directory by itself is not success evidence.

## GitHub publication

Local export may omit `publish`. If present, it must contain a complete target:

```yaml
publish:
  github:
    repository: owner/public-repository
    base_branch: main
    promotion_branch: vitryna/promote
    trusted_source_branch: main
    merge:
      mode: manual
```

Base and promotion branches must differ. Omitted `merge` means manual. Auto mode
requires `method: squash|merge|rebase` and
`protection_inspection: effective_rulesets|classic`; auto-only fields in manual
mode are rejected. Credentials never belong in configuration.

The adapter rejects reserved default/base destinations, verifies the selected
protection mode, and requests native auto-merge only when explicitly configured.
For example, add the following instead of `mode: manual`:

```yaml
merge:
  mode: auto
  method: squash
  protection_inspection: effective_rulesets
```

The generated workflow renders the source/target/ref identities as control
inputs. Changing these config values requires reviewed workflow regeneration;
mismatched committed configuration fails instead of silently changing destination.
The managed promotion branch must not be the target's current default branch,
even when the configured base is a different branch. Missing public base history
must be initialized by the owner, not by Vitryna.

Reconciliation constructs the whole public tree, including deletion of target-only
files. It creates a new public commit with one public-base parent; it does not
import private commits. Public metadata uses fixed markers and the dedicated App
identity, never the private receipt or source SHA. Credentials belong only in
the source repository's protected Actions settings.

See [GitHub setup](github-setup.md) and [security model](security-model.md). Native
auto-merge cannot atomically track later private pushes; public administrators
must not repurpose the managed promotion branch during a run.
