# Temper — execution plan

Status: **CLEANUP SHIPPED TO MASTER / NEXT ALPHA NOT RELEASED**

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
| Source and build | Cleanup `5cd2ad8` and shutdown fix `3746209` are pushed to `master`; CI is green. `build/temper` is a clean macOS ARM64 development build. |
| Public binary | Signed/notarized `0.1.0-alpha.7` supports the dispatched older client. It does not contain the new direct execution runtime. |
| Catalog distribution | Signing/update/storage utilities for the earlier catalog format remain. They do not yet publish or select the maintained V3 catalog. |

The real-model witnesses predate the direct runtime cleanup. New hermetic process
tests establish shutdown behavior, not another model-performance result.

## Next delivery

**Make the new Field Kit study work through its normal bootstrap.**

1. Prepare the next macOS ARM64 alpha from the cleaned product tree using the
   existing [release contract](contracts/release.md). Check the built binary's
   direct execution commands and preserve issued-v1 lock/export behavior.
2. Publish the signed/notarized host through the existing tag workflow when
   authorized. Do not invent a new installer or distribution framework.
3. Update V3 Field Kit's pinned host version/checksum and verify its required
   primitives against that binary. The [Field Kit plan](../../v3/FIELD-KIT-PLAN.md#next-delivery)
   owns contributor-side changes.
4. Verify fresh bootstrap, replay, read-only preview and declined consent.
   With separate exact-plan consent, verify the changed prepare/serve/stop/
   private-cleanup path against the frozen study inputs before describing that
   contributor path as validated.

Pending revision 1 results are not a prerequisite for release preparation.
Preserve their original package, producer, Python, Temper and session identities.
The new client must not resume or silently replay those runs.

## Following product work

### Signed V3 catalog distribution

Connect one maintained V3 catalog snapshot to authenticated publication,
explicit retrieval/update and rollback. Reuse existing trust, signature and
storage boundaries where they fit; remove obsolete paths only after identifying
their remaining readers.

The concrete outcome is a consumer obtaining a verified catalog, explicitly
selecting a Profile and compiling an exact lock. A catalog update cannot rewrite
the user's Selection or existing Execution Lock. Results remains the assessment
owner; no qualification registry or promotion packet is needed.

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

The cleanup and shutdown fix passed those checks, all 15 Field Kit configuration
compile/inspect checks, unchanged issued exports and read-only preview. The
shutdown regression also passed ten repeated native runs. Reuse those results
for unchanged code; documentation maintenance needs link and consistency checks.

Heavy model runs, new downloads, external spending, tagged publication and live
service changes still require their own authorization. Never use sudo, widen a
user's selection, modify an unfinished experiment or replace the legacy live
stack as an incidental engineering step. No telemetry or background updater;
third-party notices stay in release assets rather than the 0BSD source tree.
