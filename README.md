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
base for Labs-promoted Field Kit experiments; then expand the broader
qualification catalog used by the wizard. `system-package` is the portable
strategy—Homebrew is only its current macOS adapter—and the exact adapter is
always target-selected, displayed, and locked. Catalog curation is
package-specific: prefer an isolated verified
release or language environment when upstream supplies a maintainable one, and
use shared system packages only for genuine system dependencies, bootstrap
tools, or a demonstrably better-maintained distribution. This is a review
policy for every Temper installation, never a Field Kit exception or runtime
fallback. On the current macOS target, Homebrew may own the small shared tool
layer, including `uv` and `hf`; uv then owns each exact Python runtime and
application environment under a Temper root. The `hf` executable is distinct
from llama.cpp's forbidden moving `-hf` model selector. Catalog snapshots can
move without rebuilding the binary, while every resolved installation stays
pinned to one immutable snapshot. Pi remains a user-managed harness: Temper
renders its selected integration but does not install Pi, Node, or a JavaScript
package manager. This puts an installed test base ahead of the wizard and
production mode machinery. Consumer-home installation, production service
control, and the wizard remain later work.

Field Kit is being rebuilt as a user-facing, agent-operated catalog rather
than another installer. Labs authors and promotes immutable experiments with
machine buckets, applicability, cost/consent metadata, bounded prompts, and
evidence rules. Its root prompt reads Temper's canonical machine facts and
suggests applicable experiments; the user opts into each one. Temper performs
the exact mechanical work and records provenance but never reads the moving
experiment catalog or decides what to try. Experiment promotion into Field Kit
and product/profile promotion into Temper are separate reviews. The boundary
and replacement gate for the original Bash kit are recorded in
[`docs/design/field-kit-experiment-boundary.md`](docs/design/field-kit-experiment-boundary.md).

The planned wizard does not force one global “best model.” The qualification
catalog may recommend several layouts for the same machine and mode, each with
an evidence-backed performance profile and visible tradeoffs. Recommendation
never selects or prefers one: the user may install any subset and explicitly
chooses which layout starts.

The provisionally approved M2 Phase C surfaces now make that boundary concrete.
C7 uses six content-addressed typed profile documents with immutable status
history, versioned machine buckets, structured performance evidence, and
unordered plural recommendation sets. C8 is a separate one-packet/one-profile
Labs product-promotion contract whose pure compiler excludes raw/private
evidence and cannot add recommendation, consent, or selection. The owner
approved both Temper-side designs provisionally on 2026-08-25 so refinement and
fake-fixture C7 implementation can proceed. C8 still requires an explicitly
authorized Labs-side adoption before its compiler is built; there are no
seeded qualification rows yet.

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
injected command runner, with one timeout and strict closure/hash refusals. Its
production process edge now invokes no shell and forces Homebrew auto-update,
analytics, prompts, and incidental GitHub API access off. A read-only catalog
selector verifies either the active snapshot or an injected embedded fallback,
and resolution derives exact-tested, policy-eligible-untested, known-bad, or
outside-policy status without writing that status anywhere. A bounded HTTPS
catalog source now implements the signed publication transport convention. The
production Ed25519 public trust root, signed sequence-1 bootstrap, GitHub Pages
channel root, and bounded `temper software catalog update` command are compiled
and hermetically verified; the signed stable channel and immutable snapshot tree
are staged under `docs/catalog` but are not yet deployed. The retained,
release-only `temper-catalog` command validates, signs, atomically writes, and
verifies those exact publication bytes while accepting the private seed only on
stdin; no private signing material enters the tree. The uv resolver
now reads the bounded uv 0.12.x protocol, locks an exact uv-managed CPython
build and wheel-only dependency closure, and refuses incomplete or drifting
provider facts. The selected `release-artifact` method now has a
deterministic `upstream-release` resolver, a bounded HTTPS download edge, and an
isolated install/inspect/remove implementation. Hermetic tests prove exact
size/hash/archive/tree verification, atomic pointer publication, repair,
second-run cleanliness, and scope-only removal; no network or real package
manager is invoked by the suite. Python interpreter identity is no longer
ambient: a uv recipe constrains a `cpython` dependency and the exact uv-managed
interpreter is an ordinary hashed unit in the locked isolated closure. The
compiled Homebrew edge is an available shared adapter, not Temper's default
application-installation method. The first Phase B pure install planner is also
executable: it groups the complete lock by adapter/scope, orders dependencies,
isolates each named base or experiment installation, verifies required base
receipts, consumes direct or catalog-backed experiment locks without catalog
reads, shares exact global packages through root-wide claims, converges on
receipt/prepared state, and atomically republishes only wholly Temper-owned
isolated groups. Strict canonical C6 receipt and root-state documents,
derived-path conditional atomic stores, and the prepare/effect/inspect/
receipt/finalize orchestration are now
executable behind keyed injected adapters. Hermetic fake adapters prove dry-run
purity, clean second runs, live-operation refusal and expired recovery,
required-base drift refusal, pre-existing preservation, and shared claims
without reinstall. The read-only software check core is also executable: a
pure analyzer and thin reader classify provider, receipt, requirement, claim,
and prepared-operation state without creating or changing the root.
Provenance-guided removal is executable behind keyed adapters, including
serialized final-claim retirement, dry-run purity, pre-existing preservation,
conditional receipt release, and interrupted-run recovery. Canonical machine
facts and the pure ordered Field Kit Temper-material binding are now executable
too: the binding hashes exact Temper/manifest-lock bytes, names the rendered
generation, and carries recursively explicit software lock/receipt identities.
Field Kit will add the independently owned promoted-experiment, consent,
attempt, decision, and report identities around that stable material layer.
The frozen public `temper software install`, `check`, and `remove` surface is
now wired to exact macOS host-target detection and the compiled
`upstream-release` member. A hermetic command-level round-trip proves dry-run
purity, clean second runs, stable C11 output, and refusal of target mismatch or
an uncompiled locked adapter. The real release-adapter scratch round-trip has
now passed in a disposable root: dry-run left the root absent, fresh exact
assets installed and checked, the second install was byte-clean, removal
preview was pure, and removal was second-run-clean. This proves the
installation lifecycle, not an `exact-tested` catalog claim.

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

An already-resolved release-artifact software lock uses the separate installed
base surface:

```sh
# Explicitly read, authenticate, and preview the stable software catalog.
# Remove --dry-run to atomically activate a changed snapshot under this root.
go run ./cmd/temper software catalog update \
  --root /path/to/isolated/temper-root \
  --dry-run

# Preview the complete provider/receipt plan without creating the root.
go run ./cmd/temper software install \
  --lock /path/to/software.lock.yaml \
  --installation field-kit-base \
  --root /path/to/isolated/temper-root \
  --dry-run

# Audit the exact provider, receipt, requirement, and claim state.
go run ./cmd/temper software check \
  --lock /path/to/software.lock.yaml \
  --installation field-kit-base \
  --root /path/to/isolated/temper-root

# Preview provenance-guided release; remove --dry-run only when intended.
go run ./cmd/temper software remove \
  --lock /path/to/software.lock.yaml \
  --installation field-kit-base \
  --root /path/to/isolated/temper-root \
  --dry-run
```

The public binary currently compiles only the reviewed `upstream-release`
installation member. A lock naming Homebrew, uv, or another unbuilt installer
is refused; Temper never falls back to a different method or adapter.

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
- [Craft field notes](docs/craft-skill-field-notes.md) — the secondary
  through-1.0 record of how the design skills performed in real product work.
- Native verb contracts: [apply](docs/contracts/apply.md),
  [resolve](docs/contracts/resolve.md), [fetch](docs/contracts/fetch.md),
  [update](docs/contracts/update.md), and [check](docs/contracts/check.md).
- Approved M2 installed-base contract: [software install, prepared recovery,
  and installation receipt](docs/contracts/software-install.md).
- Approved M2 catalog command: [software catalog update](docs/contracts/software-catalog-update.md).
- Retained release tool: [software catalog signing and verification](docs/contracts/catalog-signing.md).
- Approved M2 surface: [software supply, independent catalog lifecycle, lock,
  and adapter-family design](docs/design/software-supply-schema.md).
- Provisionally approved M2 Phase C surface: [typed qualification catalog and profiles](docs/design/qualification-catalog-schema.md)
  plus the separate [Labs product-promotion contract](docs/design/product-promotion-contract.md).
- [Current-posture render acceptance](docs/acceptance/current-posture-render.md)
  — the concrete legacy-to-native comparison and reviewed differences.
- [Working rules](AGENTS.md) — boundaries and the product quality bar.

## Boundaries (the org)

| Repo | Role |
|---|---|
| `temper-sh/labs` | authors experiments, promotes bounded experiment packages, reviews evidence, and produces product-promotion packets |
| `temper-sh/results` | explains reviewed evidence to people |
| `temper-sh/field-kit` | offers immutable Labs-promoted experiments to consenting users and executes them over Temper |
| **`temper-sh/temper`** | **ships reviewed configuration and the minimum probe environment** |

Temper consumes reviewed packets and accepted product handoffs, never a
moving Labs directory. Reports and witnesses flow back through Labs review;
nothing here phones home, runs a daemon, or invokes sudo.

## License

[0BSD](LICENSE). The tree redistributes no third-party file.
