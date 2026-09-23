# Temper — execution plan

Status: **GUIDED SETUP IMPLEMENTED — UNRELEASED**

Updated: **2026-09-23**

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
| Guided setup | `temper init` provides modes-first Bubble Tea screens, explicit per-model templates and contexts, software choice, combined review, atomic save and optional preparation. `~/.temper` is the default; exact saved locks support offline resume. [The setup contract](contracts/init.md) owns the implemented scope. |
| Software selection | Source records are separate from resolved releases. `catalog compile --software recorded\|latest\|tested` supports the current llama.cpp/llama-swap macOS ARM64 sources. Recorded inputs are the offline default; fallback is explicit. |
| Installation | Exact isolated release/Python installation, receipts, version checks and recovery remain. System-managed software is always retained. Removing the unused Homebrew reader did not add a new Homebrew or Linux installer. |
| Field Kit host | `execution inspect/prepare/render/serve/remove` and supervised probes are implemented. The [runtime contract](contracts/execution-runtime.md) owns process identities, listener checks and final shutdown proof. |
| Source and build | Catalog delivery `a9b4a7f` is pushed and tagged alpha.10. Guided setup and its follow-up corrections are committed in source and await release; `build/temper` is the verified development build. |
| Public binary | Signed/notarized [0.1.0-alpha.10](https://github.com/temper-sh/temper/releases/tag/v0.1.0-alpha.10) adds catalog commands. Its downloaded checksum, signing identity, notarization and live-catalog compilation pass. Field Kit `01dd867` keeps its verified alpha.9 host; alpha.7 remains the dispatched revision 1 host. |
| Catalog distribution | The [stable channel](https://temper-sh.github.io/temper/catalog/channels/stable/channel.yaml) publishes signed sequence 2 with one Qwen profile. Explicit update, inspection, selection, compilation and offline rollback are delivered. [The catalog guide](CATALOG.md) owns use. |
| Compact candidate | [The authoring catalog](../catalog/README.md) adds Qwen3.5 4B as either a main model or an on-demand utility helper. Exact weights have prior assessment; the new engine/configuration and low-memory fit remain unmeasured. It is not published. |

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

The catalog delivery verified the Pages-served bytes with the existing signing
trust root. A fresh update, unchanged replay, no-write dry run, offline selection
and compilation passed. The downloaded signed alpha.10 compiled byte-identical
inputs to the verified candidate. Hermetic tests cover interrupted staging,
signature failures, stale/equivocating publications, explicit rollback with a
retained highest sequence, writer contention and preserved user files. The
recorded Qwen inputs and unknown required/tested boundaries were not changed.

## Next delivery

**Incorporate reviewed context findings, validate the compositions, then release guided setup.**

Catalog context guidance is implemented in source. The catalog separates the
model ceiling from the authored window and reviewed test points. Setup resolves
software and templates, then chooses the largest point applicable to the
detected hardware and memory allowance. The finding binds model/template bytes,
engine, router, context, output allowance and launch/request settings. Latency
guidance remains separate. There is no interpolation or new memory estimator.
The [catalog contract](contracts/execution-lock.md) owns the fields and matching
rules; the [product policy](SPEC.md) owns the intended default.

The Context tab starts at `auto`; explicit numbers remain available up to the
model ceiling. Review requests an explicit value when no finding matches and
labels its fit unknown. The accepted number is frozen in Selection and the
execution lock. The authoring candidate retains authored 32k/16k configurations
and separate 262,144-token ceilings, but **contains no reviewed machine-context
findings yet**. The signed stable catalog and saved configurations remain
unchanged. Benchmark windows are not universal defaults or machine ceilings.

Model descriptions are implemented as editable artifact metadata, shared by
main/helper choices. The chooser shows the description and optional assessment
link separately from file-size and memory facts. Workshop's
[description editor](../../v3/workshop/README.md#edit-a-model-description) calls
`temper catalog describe` to edit an explicit authoring file atomically.
`--if-empty` preserves owner wording when offering a suggestion. Description
changes leave execution identity and measured facts intact.

The [current Field Kit study](../../v3/field-kit/docs/experiments/qwen-machine-study.md#context-tests)
already checks actual input, correctness, continuation, Q8/Q4 KV and resource
limits. Its largest target is 131,072 tokens; passing that point establishes a
lower bound, not the machine's maximum. First dispatched results remain pending.
Workshop must review observations before they become maintained catalog facts;
the existing 102,400-token M5 observation reserves only 512 output tokens and
does not establish that window with the candidate's 4,096-token allowance.

Regression tests cover largest applicable context, hardware/allowance/software/
template/tuning mismatches, unknown evidence, overrides, independent mode
choices, exact review/save/resume and preserved execution identity. Description
tests cover preview, JSON/YAML edits, repeat runs, preserved suggestions,
concurrent edits and refusals without changing the source. Full Go tests, vet
and race checks pass. `build/temper` is rebuilt. An end-to-end Workshop/CLI check
used private copies and verified saved descriptions, context and resume. A native
terminal dry run showed descriptions, refused an unknown automatic context,
accepted an explicit 16,384-token window, scrolled review and completed with its
root still absent. Cached Qwen weights were recognized. No model process ran.

Shared Hugging Face cache integration is complete in the working tree. Preview
honors HF cache locations; preparation reuses cached files and delegates misses
to the official `hf` client, using `uv tool run` when hf is absent. HF owns its
cache writes and download recovery. Temper verifies bytes and keeps durable
hard links or cross-filesystem copies under its root, with explicit disk
allowances. Shared cache deletion remains user-controlled.

The first implementation follows the existing
[modes-first design](SPEC.md#wizard-set-and-profiles-settled-2026-08-13).
It saves one Selection/Execution Lock pair per chosen mode. Review binds the
exact resolved software bytes; accepting a preview never re-resolves moving
latest releases. Templates are explicit per-layout choices, including the
model's embedded template. Utility profiles leave the foreground with the
harness and expose their local helper without a generic main-model route.

The immediate remaining work is:

1. Review available and returned context observations through Workshop before
   adding applicable findings to the catalog. Keep missing measurements unknown;
   automatic matching is implemented, while numerical recommendations still
   need evidence for the exact output allowance and runtime configuration.
2. Run a separately authorized bounded native check of the compact candidate
   with its exact new engine and launch controls. Prior assessment used a
   different engine/configuration, so it does not establish this composition.
3. Obtain real low-memory observations before claiming 8/16 GiB runtime fit.
   Synthetic machine facts prove eligibility logic, not model behavior.
4. After review, publish the signed catalog candidate and the next signed binary.
   The public alpha.10 and stable catalog remain unchanged until that release.

The first wizard offers one profile per mode and requires a single layout for
its foreground execution path. The broader design's tools, integrations,
arbitrary model composition and managed mode transitions remain subsequent
work. Pi keeps its existing home and live configuration; setup does not activate
an integration or migrate a root. Managed activation and live service cutover
remain separate.

Verification of the working tree passed the complete Go tests, vet and race
checks. A real terminal check covered two-mode selection, long-review scrolling
and save/cancel dry runs; scripted save, unchanged replay and exact offline
resume passed in a private root. A fresh signed-catalog preview left its root
absent. Unsigned build/package replay, the archive checksum and all linked
third-party notices passed; a Linux cross-build checked compilation only.
No new model run or live-service change was part of this verification.

Review corrections now reuse verified model bytes across template/layout
variants and count them once in the disk preview, preserve a local layout named
`external`, and keep wrapped selection rows visible in small terminals.
Regression tests cover shared-file identity, corruption, failed/canceled
template preparation, dry runs, unrelated damaged sets, local routing/Pi
defaults and viewport navigation. Full Go tests, vet and race checks pass after
these fixes; existing catalog locks and artifact receipts retain their formats.

The Latest preview correction follows llama.cpp's downloadable numbered nightly
builds separately from its semantic stable-release headings. The wizard states
that policy; llama-swap keeps stable-release discovery. Regressions cover the
real release shapes through the wizard preview, bounded pagination, exact
nightly selection and integrity refusal without automatic fallback. A no-write
upstream preview resolved and verified `b11132` / `v257` on 23 September and left
its root absent; no model files were fetched or software executed. Full Go tests,
vet and race checks also pass after this correction.

The terminal presentation uses Lip Gloss with Tokyo Night colors, bordered
choices and persistent tabs, actions and help. Templates screens are omitted
where no alternatives exist, while accepted defaults stay explicit. Choice
screens have a keyboard/mouse Next button; Review scrolls with ordinary arrows
or a trackpad, with Left/Right selecting actions. Downloads leads with a weight
transfer summary and a collapsible file/size/status table. Inspection of the
selected Temper root and shared HF cache distinguishes cached weights from missing files;
software remains a conservative allowance until preparation. Regression tests
cover skipped screens and preserved defaults, explicit selections, cache
disclosure, mouse coordinates, narrow/short windows, full filenames and refusal
to confirm hidden actions. Full Go tests, vet and race checks pass. A native
terminal dry run exercised mouse choices and Next, skipped templates, table
expansion/collapse, arrow/trackpad scrolling, resize and Save, leaving its root
absent. This remains part of the unreleased wizard.

The shared-cache regressions cover environment precedence, exact scope passed
to hf/uv, offline cache reuse, corrupt bytes, cancellation of child processes,
retained HF retry state, independent installation/cache removal and separate
filesystem allowances. Full Go tests, vet and race checks pass; the changed
cache/fetch/planner packages compile for Linux. The installed hf 1.28.0 accepted
the invocation against a tiny offline fixture. A read-only native Qwen preview
recognized 17,559,178,144 cached model bytes, no additional weight copy and
32,908,194 remaining template/runtime download bytes, leaving its root absent.
`build/temper` is rebuilt. No real weights were downloaded or model process run.

The printed execution command runs a temporary supervised session. Engine idle
eviction or restart ends it after first use. Persistent helper availability
requires an explicit managed-lifecycle design; do not relax historical Field
Kit supervision as an incidental wizard change.

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

### Managed activation

Guided setup must select models, patches, tools and integrations deliberately,
create the user's configuration once, and propose later changes as diffs.

Production start/stop, service installation, transitions, leases and harness
integration remain subsequent work. They need concrete interruption/reload
behavior and an explicit cutover decision. Preserve existing manifest behavior
while designing that path against V3 Profile/Selection semantics.

## Open decisions for those later deliveries

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
