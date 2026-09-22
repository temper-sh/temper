# Temper — execution plan

Status: **ALPHA.9 AND FIELD KIT BOOTSTRAP DELIVERED**

Updated: **2026-09-22**

[`SPEC.md`](SPEC.md) retains the product specification and live-path contracts.
[V3 requirements](../../v3/REQUIREMENTS.md) govern the current catalog and
ownership decisions; the [workspace plan](../../v3/PLAN.md) owns cross-project
order. This file owns Temper engineering status and the next useful deliveries.

The former numbered milestone/decision plan is retained in Git through
`3746209:docs/PLAN.md`. Completed task lists and superseded qualification,
promotion, resolver and bootstrap proposals are no longer an active backlog.

## Current state

| Surface | State and owner |
|---|---|
| Native configuration workflow | Manifest/lock validation, resolve, fetch, apply, update, check, rendering and machine facts are implemented. [The explicit workflow](EXPLICIT-WORKFLOW.md) owns use; [the acceptance record](acceptance/current-posture-render.md) owns prior real-runtime evidence. |
| Maintained catalog | Artifact, Patch, Engine, Layout and Profile compile with an explicit Selection to a self-contained Execution Lock. [The execution-lock contract](contracts/execution-lock.md) owns v2 and issued-v1 compatibility. |
| Software selection | Source records are separate from resolved releases. `catalog compile --software recorded\|latest\|tested` supports the current llama.cpp/llama-swap macOS ARM64 sources. Recorded inputs are the offline default; fallback is explicit. |
| Installation | Exact isolated release/Python installation, receipts, version checks and recovery remain. System-managed software is always retained. Removing the unused Homebrew reader did not add a new Homebrew or Linux installer. |
| Field Kit host | `execution inspect/prepare/render/serve/remove` and supervised probes are implemented. The [runtime contract](contracts/execution-runtime.md) owns process identities, listener checks and final shutdown proof. |
| Source and build | Cleanup and runtime fixes are pushed. Alpha.9 source is `db8f258`; Go tests, vet, race checks and release CI pass. `build/temper` is the current macOS ARM64 development build. |
| Public binary | Signed/notarized [0.1.0-alpha.9](https://github.com/temper-sh/temper/releases/tag/v0.1.0-alpha.9) is published and pinned by Field Kit `01dd867`. Fresh bootstrap, unchanged replay, preview, declined consent and interactive setup pass. Alpha.7 remains the dispatched revision 1 host. |
| Catalog distribution | Signing/update/storage utilities for the earlier catalog format remain. They do not yet publish or select the maintained V3 catalog. |

The 22 September native check used the frozen `qwen-machine-study@2` lock and
cached model on Apple M5 / 32 GiB. Preparation replay and rendering agreed; one
64-token-bounded request completed, both owned groups stopped, and private
cleanup completed. The check observed no swap growth or watcher errors. It
establishes runtime integration, not study performance or portable defaults.

The first alpha.8 attempt failed before inference: llama-swap starts its engine
in a separate process group and `ps` reports its command by basename. Shutdown
was not proved, so cleanup was refused. The exact owned processes were stopped
after independent identity verification. The fix uses kernel executable paths,
binds each observed group, and has a native regression matching that topology.

## Next delivery

**Signed V3 catalog distribution.**

Connect one maintained V3 catalog snapshot to authenticated publication,
explicit retrieval/update and rollback. Reuse existing trust, signature and
storage boundaries where they fit; remove obsolete paths only after identifying
their remaining readers.

The concrete outcome is a consumer obtaining a verified catalog, explicitly
selecting a Profile and compiling an exact lock. A catalog update cannot rewrite
the user's Selection or existing Execution Lock. Results remains the assessment
owner; no qualification registry or promotion packet is needed.

The normal Field Kit bootstrap is delivered. Pending revision 1 results are
independent of this catalog work. Preserve their original package, producer,
Python, Temper and session identities; the new client must not resume or
silently replay them. The [Field Kit plan](../../v3/FIELD-KIT-PLAN.md#next-delivery)
owns the remaining study work.

## Following product work

### Additional engine closures

Rapid-MLX, MLX-VLM and vLLM-Metal remain experimental. Add a closure when a named
consumer needs it, including exact Python/dependency material, typed launch
controls, parser acceptance, readiness and actual loaded/effective-runtime
observation. A reachable port alone is insufficient.

Reuse unchanged runtime evidence. Changed closures require an authorized native
smoke before support claims. Generic CUDA/Linux vLLM needs an appropriate device;
keep the interface portable without claiming untested targets work.

### Guided setup and managed activation

A user-facing setup path follows the usable catalog and explicit selection
flow. Select models, patches, tools and integrations deliberately; offer
evidenced alternatives without choosing for the user. Create the user's
configuration once and propose later changes as diffs.

Production start/stop, service installation, transitions, leases and harness
integration remain subsequent work. They need concrete interruption/reload
behavior and an explicit cutover decision. Preserve existing manifest behavior
while designing that path against V3 Profile/Selection semantics.

## Open decisions for those later deliveries

- Whether to adopt `~/.temper` as the default configuration/state home; current
  operations remain location-neutral with explicit roots.
- Lease expiry and the proposed human-only force behavior for managed activation.
- Whether remote-provider integrations remain strictly render-only, and which
  concrete harness packaging/integration is worth supporting first.
- Which evidenced configurations and activation behaviors the first guided
  release can responsibly offer.
- Homebrew or other system-installer adapters only when a concrete prerequisite,
  support package or non-Python application needs one. Python uses the Python/uv
  path; Node and harness executables remain user-managed in the current scope.

These questions do not reopen the accepted software policy or justify a generic
installer framework before a consumer exists.

## Completed simplification

The [workspace cleanup record](../../v3/PLAN.md#temper-simplification-session--2026-09-21)
owns the coordinated outcome across Temper, Labs and Field Kit.

- Retired the unused qualification/product-promotion subsystem and unwired
  generic software resolution/status/bootstrap paths.
- Simplified source and execution identities while keeping download integrity,
  exact installed state and historical evidence reproducible.
- Separated minimum required compatibility from minimum tested evidence.
  Both latest and tested selection satisfy applicable requirements; unknown
  floors stay unknown. The [Workshop procedure](../../v3/workshop/SOFTWARE-UPDATES.md)
  owns how to establish those facts; Temper owns the catalog data.
- Removed system-package deletion and retirement. Notices may say a package is
  no longer needed by Temper; removal remains the user's independent action.
- Moved direct execution and process supervision into Temper. Field Kit retains
  experiment decisions and observations.
- Fixed macOS process-exit observations: re-read incomplete command observations,
  retain known exiting identities, and wait for reaping/group absence before
  allowing private cleanup. Unknown, reused or escaped identities still refuse.

The old signed-publication tooling and real installer acceptance evidence remain
where still useful. Their existence is not an instruction to restore retired
product architecture.

## Design discipline

The [repository instructions](../AGENTS.md) select the relevant Guild craft
guidance. Apply it to the concrete decision, without adding a checklist or
schema inventory to the product.

| Kind | Temper examples |
|---|---|
| Pure computation | Catalog/Selection compilation, native rendering, diffs, budgets and installation plans. |
| Read | Machine facts, upstream resolution, receipt checks, process/listener observations and status. |
| Side effect | Verified downloads, installation, atomic lock/config/state publication and owned process lifecycle. |

CLI verbs orchestrate these boundaries explicitly. `check` and `status` do
not repair state; an update does not silently install, activate or restart.

- Stage and validate before committing. Resolve/update changes one complete
  lock; apply publishes a complete immutable generation; fetch verifies material
  before publication.
- Installation records prepared intent before provider effects and reconciles
  interrupted effects through inspected state and receipts. Retain shared state
  only for installation, version checks, recovery and concurrency.
- Process operations act on verified owned identities and require an explicit
  final shutdown result. A status file alone never grants signaling authority.
- Dry run never mutates, and an identical successful second run is clean.
- Business refusals explain the failed precondition; operational failures retain
  useful context; invalid internal states fail loudly. Retry only transient
  reads/effects that are safe to repeat.
- Change a schema or public contract for a real consumer. Update the owning
  contract and [code map](CODE.md) when ownership or operation paths change.

The accepted secondary objective through 1.0 remains: record concise,
evidence-linked [craft field notes](craft-skill-field-notes.md) at substantive
delivery closeouts. Record useful guidance, defects and any narrow proposed
improvement, or no change. This adds no synthetic work, model evaluation or
automatic skill-editing task.

## Verification and effect boundaries

For product changes, run formatting/whitespace checks, relevant contract and
failure-boundary tests, the full Go tests, `go vet` and race tests. Verify
dry-run purity, interruption/refusal and clean reruns at the affected effect
boundary. Use native harmless child processes for supervisor tests.

The cleanup passed all 15 Field Kit configuration compile/inspect checks and
unchanged issued exports. The alpha.9 runtime fix passed full tests, vet and race
checks, three repeated supervisor race runs, and the bounded native check above.
Both repositories' CI and the signed release workflow pass. Reuse those results
for unchanged code; documentation maintenance needs link and consistency checks.

Heavy model runs, new downloads, external spending, tagged publication and live
service changes still require their own authorization. Never use sudo, widen a
user's selection, modify an unfinished experiment or replace the legacy live
stack as an incidental engineering step. No telemetry or background updater;
third-party notices stay in release assets rather than the 0BSD source tree.
