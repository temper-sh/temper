# `manifest.lock.yaml` — schema design (M1 item 1)

Rewritten 2026-08-14 after owner review ("insanely overengineered" —
correct). The first draft accreted M2/M4 structure — a profiles catalogue,
tool sections, per-binding witnesses — into a v1 that has no catalog, no
tool entries and no modes to describe. v1 locks what exists today and
nothing else; the schema grows by reviewed revision when the consuming
milestone lands ("model the fields you know"). The decisions from that
review survive as the closing list, as directions rather than structure.

## What v1 must do

1. Stop `-hf` following a moving branch: pin revision and file hashes per
   manifest entry, without forcing a download.
2. Record the exact pins so "am I on a tested configuration?" is
   answerable by *comparison*: each temper release ships a database of
   tested versions (the M2 catalog), and `check` compares the lock's pins
   against it (owner ruling 2026-08-18 — tested-status is shipped
   knowledge, derived at read time, never a locally stored flag).
3. Let `check` see drift: manifest changed after pin, an entry with no
   row, a row with no entry.

## Schema (`temper-lock/v1`)

```yaml
schema: temper-lock/v1
entries:
  <layout-id>:                # the key under manifest.yaml `layouts:`
    repo: <manifest model.repo value at resolve time>
                              # kept so `check` can see the manifest moved
                              # after the pin (a snapshot, not a copy)
    revision: <hf commit sha>
    files:
      - name: <filename>
        sha256: <hex>         # from HF metadata — pinning never downloads
    patches:                  # only when the manifest entry lists any
      - name: <patches/ dir name>
        sha256: <hex of the final transformed patch bytes>
    resolved: <date>
```

Hash *verification* of multi-GB files present on disk is on demand
(`check --verify`, the legacy `setup.sh --verify` pattern) — never routine.

## Writers

- Bootstrap history: the owner initially maintained reviewed rows manually
  beside the manually maintained manifest. `apply` remains read-only over both
  inputs and refuses gaps or drift.
- `temper resolve` now fills missing rows and never touches
  existing ones. Resolution is a separate atomic lock commit; `apply` keeps
  its one rendered-world commit and remains a strict consumer.
- `update [id]` now rewrites the whole row when its identity moves, prints the
  old→new diff and the entry's targeted gate — never runs it. Once the catalog
  exists (M2) it also says when the new pin leaves the active catalog's tested
  set.
- `check` — now reads and reports repo/selection mismatch, missing rows,
  orphan rows, and selected-mode artifact admission; `--verify` adds full
  hash checks of selected files. From M2 it also reports tested-set membership
  per entry.

There is no `attest` verb and no local verified state — D13 closed
2026-08-18: tested-status is derived by comparing pins against the
signed catalog, so nothing ever needs to write it.

## Test plan (hermetic, canned resolutions, no network)

1. Bootstrap slice: `apply && apply` — the second run changes nothing; a
   missing row or repo mismatch refuses without rewriting either input.
2. Resolver slice (implemented): one new manifest entry → exactly one row added, existing
   row values identical; a concurrent lock change refuses instead of losing
   either writer.
3. `update <id>` (implemented) → that row moves, diff and gate text printed,
   others untouched; a clean second run retains its original resolution date.
4. Orphan row and repo mismatch → reported, never auto-fixed.

## Deliberately absent, and when each arrives

Each item lands as a reviewed `temper-lock` schema revision with its
milestone — expand when there is something to protect.

- **Tested-status in the lock** → never. Owner ruling 2026-08-18:
  independently published signed snapshots carry the database of tested
  versions (the M2 catalog);
  "on/off the tested set" is `check`'s comparison, not a stored field.
  This replaced the first draft's `verified:` field and the `attest` verb
  (D13, closed).
- **Local attestation records** ("I gated this myself after moving past
  the tested set") → only if a real need appears; today an off-set pin is
  simply reported as absent from the relevant catalog's tested set.
- **Machine identity block** → with `~/.temper` (D3), when a lock synced
  between machines first becomes possible.
- **Profile citations** → with the catalog (M2). Agreed shape: a
  top-level `profiles:` catalogue where each cited revision lists the
  entries it carries — composition visible at the composer, never
  per-entry back-references.
- **Tool entries** → with tool profiles (M2), as their own section
  (models and tools are resolved and gated too differently for one union
  shape), ids sharing one namespace with models.
- **Mode/tuning records** → never in v1. Modes landed in the native manifest
  before lock resolution did; tuning remains intent in the manifest and
  derived harness values remain rendered output. A later schema may pin
  per-mode rendered-config hashes, but it still does not copy tuning values
  into the lock.
- **Witness detail** (gate ids, conditions, engine-version snapshots) →
  only if a real dispute ever needs it; the session log carries it today.
