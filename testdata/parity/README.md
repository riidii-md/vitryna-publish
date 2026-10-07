# Synthetic parity fixture

All contents here are invented. `source.json` creates a committed source fixture
with a public README, executable script, CRLF file, and private notes.
`legacy-policy.json` uses Tatami's schema; `vitryna.yml` mechanically maps those
rules, including the explicit legacy `docs/private` reservation.

`normalized-files.json` is the independent literal `(path, mode, size, sha256)`
oracle shared by both exporters. Integration tests compare complete records,
exported bytes/modes, immutable source semantics, seal round-trip, and private
exclusion. They do not prove parity on a real private Tatami commit. That later
comparison remains private and is described in `docs/migration-tatami.md`.
