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
  <manifest-entry-id>:        # the entry's own `id:` from manifest.yaml
    repo: <manifest repo value at resolve time>
                              # kept so `check` can see the manifest moved
                              # after the pin (a snapshot, not a copy)
    revision: <hf commit sha>
    files:
      - name: <filename>
        sha256: <hex>         # from HF metadata — pinning never downloads
    patches:                  # only when the manifest entry lists any
      - name: <patches/ dir name>
        sha256: <hex of the fetched file>
    resolved: <date>
```

Hash *verification* of multi-GB files present on disk is on demand
(`check --verify`, the legacy `setup.sh --verify` pattern) — never routine.

## Writers

- `apply` — fills missing rows; never touches existing ones.
- `update [id]` — rewrites the whole row, prints the old→new diff and the
  entry's targeted gate — never runs it. Once the catalog exists (M2) it
  also says when the new pin leaves the release's tested set.
- `check` — reads and reports: repo mismatch, missing rows, orphan rows;
  from M2, tested-set membership per entry; `--verify` adds hash checks
  of present files.

There is no `attest` verb and no local verified state — D13 closed
2026-08-18: tested-status is derived by comparing pins against the
shipped database, so nothing ever needs to write it.

## Test plan (hermetic, canned resolutions, no network)

1. `apply && apply` — the second run changes nothing.
2. One new manifest entry → exactly one row added, others byte-identical.
3. `update <id>` → that row moves, diff and gate text printed, others
   untouched.
4. Orphan row and repo mismatch → reported, never auto-fixed.

## Deliberately absent, and when each arrives

Each item lands as a reviewed `temper-lock` schema revision with its
milestone — expand when there is something to protect.

- **Tested-status in the lock** → never. Owner ruling 2026-08-18: each
  temper release ships the database of tested versions (the M2 catalog);
  "on/off the tested set" is `check`'s comparison, not a stored field.
  This replaced the first draft's `verified:` field and the `attest` verb
  (D13, closed).
- **Local attestation records** ("I gated this myself after moving past
  the tested set") → only if a real need appears; today an off-set pin is
  simply reported as untested-by-release.
- **Machine identity block** → with `~/.temper` (D3), when a lock synced
  between machines first becomes possible.
- **Profile citations** → with the catalog (M2). Agreed shape: a
  top-level `profiles:` catalogue where each cited revision lists the
  entries it carries — composition visible at the composer, never
  per-entry back-references.
- **Tool entries** → with tool profiles (M2), as their own section
  (models and tools are resolved and gated too differently for one union
  shape), ids sharing one namespace with models.
- **Mode/tuning records** → with modes (M4). Agreed rules: tuning values
  never enter the lock (intent is the manifest's mode-first `modes:`
  section; advice is the catalog); the lock pins per-binding
  rendered-config hashes and witnesses over one artifact pin. Rendered
  artifacts include the harness-side materializations — e.g. Pi's
  compaction sizing, derived per mode from the selected model's window
  (legacy FINDINGS #25) — and #25 is why the values stay out: its failure
  *was* a stored constant meeting a different window.
- **Witness detail** (gate ids, conditions, engine-version snapshots) →
  only if a real dispute ever needs it; the session log carries it today.
