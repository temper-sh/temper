# `temper check` — lock, artifact, and budget audit

Status: executable M1 contract, 2026-08-20.

`check` answers whether one manifest, lock, selected mode, and local Temper
root agree, and whether the selected resident posture is predicted to fit this
machine's Metal wired-memory wall. It is a read-only, offline report: it never
resolves upstream, downloads, repairs, rewrites, renders, changes machine
limits, or touches the running service.

## Invocation

```text
temper check --root PATH
  [--manifest manifest.yaml]
  [--lock manifest.lock.yaml]
  [--mode local]
  [--verify]
```

`--root` is required and never inferred from the legacy installation. The
manifest and lock paths default beside the invocation. The mode is explicit
input to the audit and defaults to `local`; Temper never chooses another mode.

## Audit scope

Every run strictly parses both documents, then checks lock drift across the
whole manifest:

- every manifest layout has a lock entry;
- the lock has no entry for an absent manifest layout; and
- each row still selects the manifest's exact repo, model file, and optional
  transformed patch.

For every resident and on-demand member of the selected mode, routine check
then applies the same artifact admission contract as `fetch` and `apply`:
canonical receipt identity, exact regular-file shape, no symlinks or extras,
and sizes matching the receipt written after fetch-time hashing.

`--verify` first performs that admission check and then streams every selected
model and transformed patch, comparing its SHA-256 directly with the lock. It
is the explicit, potentially multi-GB full-byte audit; supplying the flag is
the request to pay that I/O cost. It remains offline and read-only.

Check accumulates independent findings so one bad layout does not hide the
next. A selected layout whose lock row is missing or drifting is reported as
failed without guessing an artifact identity.

## Budget prediction

Every completed audit also emits the M1 wall model defined in
[`docs/design/wall-model.md`](../design/wall-model.md). The prediction uses:

- the manifest's `gpu_memory_utilization` policy;
- physical memory and the live `iogpu.wired_limit_mb` machine read (falling
  back to an explicitly labeled macOS-default prediction when absent); and
- model-byte sizes from successfully admitted resident artifact sets.

The preferred GPU-resident coder is the allocation holder. Other resident
members with GPU placement are co-tenants; `ngl: 0` and all on-demand members
are excluded. The holder envelope is the greater of its fraction of predicted
Metal device memory and its model-file lower bound. The envelope, co-tenants,
and a 1,024 MiB OS/transient policy floor must fit the wired limit.

This is always labeled `prediction`. It is not a runtime measurement or a
catalog qualification. A mode with no GPU-resident local foreground is
`not-applicable`. A resident artifact admission failure makes it `unavailable`
rather than allowing unknown bytes to count as zero.

## Output and exits

The first stdout line is always the summary for a completed audit:

```text
RESULT check ok|failed mode=<id> verification=receipt|sha256 layouts=<n> problems=<n>
```

Selected layouts follow in stable ID order:

```text
LAYOUT <id> ok|failed artifact-set=<digest|none> files=<n>
```

`files` is the number of data files selected by the matching lock row, not a
claim that every file passed when the layout status is `failed`.

One budget line follows the layouts:

```text
BUDGET prediction fits|exceeded holder=<layout-id> physical-mib=<n> device-mib=<n> utilization=<fraction> allocation-mib=<n> holder-minimum-mib=<n> co-tenants-mib=<n> os-floor-mib=<n> required-mib=<n> wired-limit-mib=<n> spare-mib=<signed-n> source=live-sysctl|predicted-macos-default
BUDGET prediction unavailable|not-applicable reason=<quoted-string>
```

Each finding follows in stable order. The detail is a quoted human message;
automation branches on the code and layout fields:

```text
PROBLEM code=<code> layout=<id> detail=<quoted-string>
```

The first slice defines these codes:

- `lock-entry-missing`
- `lock-entry-orphan`
- `lock-selection-drift`
- `artifact-not-materialized`
- `artifact-invalid`
- `artifact-hash-mismatch`
- `budget-exceeded`

Exit `0` means the completed audit is `ok`. Exit `1` means either the completed
audit found problems or an input/filesystem operation prevented a valid
report; fatal errors go to stderr and have no `RESULT` line. Exit `2` is CLI
usage refusal.

## Deliberately outside this slice

- upstream drift or tested-catalog membership;
- served-model comparison through llama-server `/props`;
- advisory wizard differences; and
- repair, download, activation, or service control.
