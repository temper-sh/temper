# Temper

Temper installs, tunes, and verifies a local-LLM stack on Apple Silicon — and
refuses to pretend. Every recommendation traces to a Labs decision packet, a
measurement on real hardware, and a Results record people can audit; every
number carries the conditions it ran under, and anything unmeasured is
labeled unmeasured.

This is `temper-sh/temper`, the release repository: it ships reviewed
configuration and the minimum probe environment. Setup, wizard, generator,
lock, reviewed catalog profiles, acceptance suites, machine report, harness
adapters, and the probe *base* live (or will live) here — the probes
themselves belong to field-kit, which runs its stages over the base Temper
installs.

## Status

**Pre-release product build — native M1 pin/render/check complete**
(2026-08-20). Temper strictly parses `temper-manifest/v1` and
`temper-lock/v1`, resolves missing rows without moving existing pins,
re-resolves explicitly selected existing pins without fetching or activating
them, materializes one explicitly selected immutable layout set, audits lock
and local-artifact agreement (including an explicit full SHA-256 pass), reads
the local Mac's memory allowances and emits a labeled resident-wall
prediction, renders llama-swap and Pi configuration in Go, and commits each
effect through one atomic target.

**M2 is underway in three deliberate phases:** build the minimum independently
published software-supply catalog and exact `software.lock.yaml`; use
adapter-backed installation methods to install, check, and remove a receipted
base for Field Kit; then expand the broader qualification catalog used by the
wizard. `system-package` is the portable strategy—Homebrew is only its current
macOS adapter—and the exact adapter is always target-selected, displayed, and
locked. Catalog snapshots can move without rebuilding the binary, while every
resolved installation stays pinned to one immutable snapshot. This puts an
installed test base ahead of the wizard and production mode machinery.
Consumer-home installation, production service control, and the wizard remain
later work.

The planned wizard does not force one global “best model.” The qualification
catalog may recommend several layouts for the same machine and mode, each with
an evidence-backed performance profile and visible tradeoffs. Recommendation
never selects or prefers one: the user may install any subset and explicitly
chooses which layout starts.

The M2 Phase A shared resolver core is executable: strict software-catalog and
software-lock parsing, deterministic target-to-adapter selection, compiled
adapter descriptor checks, provider-neutral candidate closures, SemVer/PEP 440
policy selection backed by pinned maintained parsers, exact closure validation,
canonical digests, and a dry-run/concurrency-safe atomic lock transaction. The
internal catalog-update slice also verifies separately signed Ed25519 channel
and catalog artifacts, refuses rollback/equivocation and unsupported compiled
capabilities, stores immutable snapshots, and atomically moves a regular-file
active pointer. The Homebrew resolver edge now translates an exact target's
recursive formula closure and `brew info --json=v1` bottle metadata behind an
injected command runner, with one timeout and strict closure/hash refusals.
Production trust/bootstrap inputs, catalog transport and process binding, the
uv resolver's Python-interpreter surface, tested-status reporting, the public
command surface, and installation remain next; no network or real package
manager is invoked by these slices.

The legacy `local-ai-setup` repo remains the installer of record for the one
machine currently running the stack until M5. These commands therefore require
an explicit, isolated data root and never infer the live installation. The
workflow steps stay separate and run in this order:

```sh
# Fill missing lock rows without moving existing pins.
go run ./cmd/temper resolve \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml

# Preview one explicit upstream pin move. Remove --dry-run to commit only the lock.
go run ./cmd/temper update qwen3.8-27b-gguf-24k \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml \
  --dry-run

# Materialize each layout selected by the intended mode, one explicit ID at a time.
go run ./cmd/temper fetch qwen3.8-27b-gguf-24k \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml \
  --root /path/to/isolated/temper-root

# Verify those immutable sets and preview the generated configuration.
go run ./cmd/temper apply \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml \
  --mode local \
  --root /path/to/isolated/temper-root \
  --dry-run

# Audit the selected mode and its predicted resident-memory fit;
# add --verify for a full multi-GB SHA-256 pass.
go run ./cmd/temper check \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml \
  --mode local \
  --root /path/to/isolated/temper-root
```

Remove `--dry-run` from `apply` to create an immutable generation under that
root. Its dry-run still requires the selected artifact sets because Temper
will not preview a configuration backed by absent or unadmitted artifacts. A
non-dry `fetch` can download multi-GB weights; it always requires exactly one
layout ID and never infers a fetch-all operation. `check` reads memory facts
but never changes the wired limit. This slice does not activate its output in
llama-swap or Pi. No release is implied by this status.

`update` reads current upstream metadata (and any small, already-pinned patch
source), commits changed lock rows once, and prints the role-specific commands
the operator should run after explicitly fetching and applying the new set. It
never downloads weights, runs those commands, or touches the service. Bare
`update` is supported but warns that it bundles independent layout risks.

## Start here

- [Product spec](docs/SPEC.md) — what Temper is, for whom, and its
  non-negotiable principles.
- [Execution plan](docs/PLAN.md) — milestones M0–M5 with acceptance gates,
  interface contracts, the design discipline, and the owner decision
  register.
- Native verb contracts: [apply](docs/contracts/apply.md),
  [resolve](docs/contracts/resolve.md), [fetch](docs/contracts/fetch.md),
  [update](docs/contracts/update.md), and [check](docs/contracts/check.md).
- Approved M2 surface: [software supply, independent catalog lifecycle, lock,
  and adapter-family design](docs/design/software-supply-schema.md).
- [Current-posture render acceptance](docs/acceptance/current-posture-render.md)
  — the concrete legacy-to-native comparison and reviewed differences.
- [Working rules](AGENTS.md) — boundaries and the product quality bar.

## Boundaries (the org)

| Repo | Role |
|---|---|
| `temper-sh/labs` | decides and gathers evidence; owns experiments, dossiers, and product handoffs |
| `temper-sh/results` | explains reviewed evidence to people |
| `temper-sh/field-kit` | executes frozen portable tests on consenting machines |
| **`temper-sh/temper`** | **ships reviewed configuration and the minimum probe environment** |

Temper consumes reviewed packets and accepted product handoffs, never a
moving Labs directory. Reports and witnesses flow back through Labs review;
nothing here phones home, runs a daemon, or invokes sudo.

## License

[0BSD](LICENSE). The tree redistributes no third-party file.
