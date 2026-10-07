# Roadmap

V1 focuses on one source → one public target, repository-owned exact/subtree
selection, deterministic committed export, immutable artifact verification,
manual PR promotion, and explicit optional native auto-merge.

The first rollout gates are a reviewed release, dedicated hosted sandbox
acceptance, and same-commit Tatami parity. None is permission to cut over a live
repository automatically.

Future directions, not current configuration promises:

- Public contribution import and possibly bidirectional synchronization. Preserve
  review, attribution, and private/public boundaries; do not auto-import arbitrary
  public changes into development.
- Multiple targets and customer-specific publication profiles, with independent
  ownership, credentials, receipts, and concurrency.
- Tag and GitHub Release propagation, explicitly separate from content PRs and
  private provenance.
- Richer feature/script selection: globs, excludes, renames, overlays, templating,
  line filtering, and transformations. Define deterministic semantics and proof
  before adding these; v1 does not strip private sections from public files.
- Reviewed setup upgrades and broader platform/provider support after the narrow
  GitHub.com/Linux/macOS contract is proven.

Auto-merge is already an opt-in v1 policy, not a blanket prohibition. Stronger
freshest-source guarantees would require a different coordination contract; the
current native auto-merge race is documented rather than hidden.
