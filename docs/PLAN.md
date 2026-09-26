# Temper — execution plan

Status: **GUIDED SETUP IMPLEMENTED — UNRELEASED**

Updated: **2026-09-26**

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
| Installation | Exact isolated release/Python installation, receipts, version checks and recovery remain. Receipt reuse now compares installed software facts independently of execution provenance. Context and other runtime changes reuse unchanged software for llama.cpp and Splash. System-managed software is always retained. |
| Field Kit host | `execution inspect/prepare/render/paths/serve/remove` and supervised probes are implemented. The [runtime contract](contracts/execution-runtime.md) owns process identities, listener checks and final shutdown proof. |
| Current router compatibility | The working tree recognizes llama-swap v257's narrowly scoped macOS inspection helpers using kernel executable/argv and router ancestry. Engine lifetime remains fixed for measurements; unknown children still refuse ownership. Full tests, race, vet and Linux compilation pass. Native catalog attempts are retained in [Labs](../../v3/labs/workstreams/model-runtime-optimization/method/catalog-refresh-2026-09-24.md). |
| Source and build | Catalog delivery `a9b4a7f` is pushed and tagged alpha.10. Guided setup, native Metal detection and Splash are committed in `3ba7813`. The software receipt simplification is committed in `98b99ce`; the Qwen study preparation adds exact Python closures and probe identities. These changes await release. |
| Public binary | Signed/notarized [0.1.0-alpha.10](https://github.com/temper-sh/temper/releases/tag/v0.1.0-alpha.10) adds catalog commands. Its downloaded checksum, signing identity, notarization and live-catalog compilation pass. Field Kit `01dd867` keeps its verified alpha.9 host; alpha.7 remains the dispatched revision 1 host. |
| Catalog distribution | The [stable channel](https://temper-sh.github.io/temper/catalog/channels/stable/channel.yaml) publishes signed sequence 2 with one Qwen profile. Explicit update, inspection, selection, compilation and offline rollback are delivered. [The catalog guide](CATALOG.md) owns use. |
| Catalog candidate | [The authoring catalog](../catalog/README.md) offers Qwen3.8, Qwen3.5 and Gemma E2B/E4B as main models, plus Qwen3.5 as an on-demand utility. It records b11157/v257 and authors 40,960 tokens for Qwen3.8 after discovery, fresh confirmation and a full-output resource check on M5/32 GiB. Small-model context and focused task observations retain b11149; they do not qualify all profiles on b11157. Optional Sharp compositions and real 8/16 GiB fit remain unmeasured. It is not published. |

The receipt simplification passes full Go tests, vet and race checks after
integration with `3ba7813`. Regressions cover llama.cpp settings and Splash
context changes retaining installed-software identity while execution identity
changes. Historical prepared operations whose digests included provenance
still require their producing runtime for recovery. All 15 current Field Kit
configurations compile unchanged, and consent planning accepts the live Metal
facts. No new model run was part of this integration.

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

The 24 September catalog investigation found another startup boundary: v257's
macOS hardware detection starts `system_profiler`, including children in separate
groups. The initial attempt refused the unrecognized child and could not prove
shutdown. Its isolated router was stopped only after independent ownership
verification; the first unsafe status remains intact. The new narrow helper
classification binds executable, exact NUL-delimited kernel argv, ancestry,
start time and group. Only the reviewed read-only inspection invocations are
accepted, and helpers never replace measured router/engine roles. Tests include
unrelated children, changed identities, write-capable arguments and engine
descendants. A subsequent native start/shutdown passed, then exposed a separate
Field Kit Python 3.9 timestamp parser defect, now repaired in that owner's tree.
These local compatibility fixes do not add persistent serving or change the
published alpha.10/Field Kit alpha.9 binaries.

The catalog delivery verified the Pages-served bytes with the existing signing
trust root. A fresh update, unchanged replay, no-write dry run, offline selection
and compilation passed. The downloaded signed alpha.10 compiled byte-identical
inputs to the verified candidate. Hermetic tests cover interrupted staging,
signature failures, stale/equivocating publications, explicit rollback with a
retained highest sequence, writer contention and preserved user files. The
recorded Qwen inputs and unknown required/tested boundaries were not changed.

## Next delivery

**Close catalog evidence gaps and plan the usable stack through Workshop.**

The authorized 24 September overnight catalog review, bounded native
investigation and managed-serving/tool/integration research are complete.
The [workspace plan](../../v3/PLAN.md#full-stack-workstreams) retains all six
layers. Guided setup is implemented; the next work establishes practical
portfolio coverage, current-composition evidence, actual machine fit and the
tool/integration offering. Publication follows a reviewed delivery decision.
Persistent service installation and live cutover are outside this research run.

Catalog context guidance is implemented in source. The catalog separates the
model ceiling from the authored window and reviewed test points. Setup resolves
software and templates, then chooses the largest point applicable to the
detected hardware and memory allowance. The finding binds model/template bytes,
engine, router, context, output allowance and launch/request settings. Latency
guidance remains separate. There is no interpolation or new memory estimator.
The [catalog contract](contracts/execution-lock.md) owns the fields and matching
rules; the [product policy](SPEC.md) owns the intended default.

The Context tab starts at `auto` where reviewed findings exist; otherwise it
asks for an explicit number before continuing. If resolved software or refreshed
machine facts invalidate an automatic choice, review returns to the affected
Context field with other selections intact. Missing evidence is an input
request, not a generic preview failure. Manual values remain bounded by the
model ceiling; unmeasured choices have unknown fit. The accepted number is
frozen in Selection and the
execution lock. The authoring candidate retains authored 40,960/16,384-token configurations
and separate native ceilings (262,144 for Qwen; 131,072 for Gemma), but **contains no canonical machine-context
findings yet**. The signed stable catalog and saved configurations remain
unchanged. Benchmark windows are not universal defaults or machine ceilings.

The 24 September [b11149 native record](../../v3/labs/workstreams/model-runtime-optimization/results/catalog-defaults-m5.json)
contains reviewed candidate fields for the earlier 16k/32k windows on the
tested M5/32 GiB, reserving 4,096 output tokens. Gemma needed a separately frozen
semantic lookup check after strict JSON controls failed on Markdown fences.
No public HTTP(S) evidence destination exists for these shadow records yet;
the current contract therefore cannot accept them as canonical findings.
Keep this concrete publication dependency separate from missing measurements.
Actual low-memory machines and optional templates remain different evidence
questions.

The [Qwen3.8 capacity result](../../v3/labs/workstreams/model-runtime-optimization/results/qwen27-capacity-m5.json)
qualifies the authored 40,960-token point on b11157/v257 with discovery and fresh
confirmation. Ordinary 36,352-token initial requests took 487.83/506.28 seconds;
continuations took 21.00/21.08 seconds, reusing 36,348 tokens and processing 94
new tokens. A forced resource test consumed 36,570 input plus all 4,096 output
tokens and ended at the length limit in 535.64 seconds. This output-budget check
is not answer-quality evidence. Confirmation peak RSS was 20.829 GiB with no
swap growth, memory-pressure, thermal or CPU-limit event and verified shutdown.
Larger stopped points do not establish a numeric ceiling. Only the authoring
candidate's Qwen context scalar and shared recorded engine release change;
the signed stable catalog, historical specimen and issued configurations retain
their older identities. Canonical context findings still await a real public
HTTP(S) evidence destination.

The [Qwen3.5 checkpoint comparison](../../v3/labs/workstreams/model-runtime-optimization/results/qwen35-checkpoint-reuse-m5.json)
supports a narrow optimization candidate: one checkpoint reduced filled-context
continuation by 89% in both order blocks, preserving all sixteen answers. Review
representative multistep and rewind behavior before changing the canonical
zero-checkpoint setting. Spark also completed five focused specimens on the
official engine; wider work and context capacity still precede a portfolio
addition. No catalog record was promoted from either diagnostic.

Catalog content now reuses the closed capability portfolio and maintained
Workshop suite. The selected GGUF files and Frog template remain current;
Sharp is an optional per-Qwen choice and preserves the existing defaults.
Recorded software is llama.cpp b11157 / llama-swap v257. Exact fallback archives
for the cited tested versions also resolve and compile. These are mechanical
checks; historical assessments retain their original engine/template/settings.
Required compatibility floors remain unknown. Context regressions use synthetic
fixtures; catalog content is checked by compiling its declared choices.

Tight memory budgets now receive a prominent wired-limit recommendation in
model selection and above the review's download summary. The same guidance is
available to scripted setup and blocked preparation. It prints bounded manual
sysctl instructions, verification, restart and rollback steps; Temper changes
no system setting. The [setup contract](contracts/init.md) owns the advisory
threshold and macOS memory reserve. That reserve is explicitly Temper policy,
not a detected maximum system override.

Live detection now queries Metal's recommended working set through the system
framework, with no Swift, Xcode or MLX dependency on the user's machine. The
cgo-free release build is preserved. Machine facts label the effective budget
`live-metal` and keep the optional raw sysctl override separate. Old canonical
facts retain their original bytes and prediction labels. Setup rereads facts
on review/retry and preparation, and manual instructions verify the resulting
Metal budget. Recommendations account for the fraction allocation growing with
an increased budget.

Focused regressions, the full race-test suite and vet pass; `build/temper` is
rebuilt. The release build and local packaging check pass, including the new
binding's license notice; the non-macOS build still compiles. Native read-only
checks on the M5/32 GiB report a 24 GiB Metal budget. The Qwen scripted preview
uses it, recognizes the shared cached weights and leaves its root absent. Its
21.40 GiB prediction has 2.60 GiB spare, so the earlier 26 GiB recommendation
from the percentage estimate is gone. This remains an admission prediction,
not a new context measurement. No sysctl change, model run or download was
performed during these checks.

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
lower bound, not the machine's maximum. The first returned revision 2 report
passed review; its baseline completed and tuning stopped before inference on
the now-corrected receipt invalidation. See the [Field Kit plan](../../v3/FIELD-KIT-PLAN.md).
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
It saves one Selection/Execution Lock pair per selected configuration. Local
models have separate install checkboxes and one explicit default. The chooser
groups estimated memory tiers largest first, then uses catalog-owned ordering
within each group, with distinct Model / Weights / Engine labels. The default
retains the `local` pair; alternatives get named pairs and private installation
IDs. No ordering edit changes execution identity or the user's saved default.
Regression checks cover multi-model save and exact offline resume, explicit
defaults, independent templates/context and presentation-only identity changes.
Full tests, vet and race checks pass. A native M5/32 GiB terminal dry run showed
S before XS, configured Qwen and Gemma independently and kept its root absent.
Review binds the exact resolved software bytes; accepting a preview never re-resolves moving
latest releases. Templates are explicit per-layout choices, including the
model's embedded template. Utility profiles leave the foreground with the
harness and expose their local helper without a generic main-model route.

The immediate remaining work is:

1. Give reviewed current-context observations a real public evidence destination
   before incorporating the applicable findings through Workshop. Automatic
   matching is implemented; retained candidate points bind exact defaults,
   output allowance and machine. Keep unmeasured choices unknown.
2. Review the checkpoint candidate on representative multistep/rewind work and
   optional Sharp compositions on affected Workshop cases when selected for
   investigation. Retained b11149 task checks and the b11157 Qwen context point
   do not cover optional templates or establish universal engine compatibility.
3. Obtain real low-memory observations before claiming 8/16 GiB runtime fit.
   Synthetic machine facts prove eligibility logic, not model behavior.
4. Use the [managed-tools findings](../../v3/labs/workstreams/managed-tools/README.md)
   to implement an explicit start/stop/status boundary when engineering begins.
   Keep the fixed-process experiment supervisor separate. Shared tool work
   needs the identified access, validation and cancellation repairs plus a
   completed-work comparison before an installable integration offering.

The wizard offers several installed local configurations and one utility
profile, and requires a single layout for each foreground execution path.
The broader design's tools, integrations,
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

The published Field Kit bootstrap remains on alpha.9; local revision 3 requires
the integrated development host until a compatible signed release ships.
Historical study results are independent of this catalog work. Preserve their original package, producer,
Python, Temper and session identities; the new client must not resume or
silently replay them. The [Field Kit plan](../../v3/FIELD-KIT-PLAN.md#next-delivery)
owns the remaining study work.

## Following product work

### Splash 1.1.0 integration

Splash 1.1.0 integration is implemented in the candidate catalog. Qwen–Splash
leads S on compatible machines; Qwen–llama remains its baseline. The prebuilt
release carries its own Python and Metal kernels. Its architecture mapping
validates the exact DFlash2 sidecar, and `execution prepare` derives the tokenizer
from the selected GGUF and applies Frog. Target and draft downloads, preparation
receipts, and runtime conversion cache have separate ownership. Serving uses
local material and supervises both Python frontend and native engine. Historical
coding results retain their tested source identity.

Verification on 26 September: full Go and race suites, `go vet`, and diff checks
passed. The actual release archive passed bounded inspection, extraction and
inventory comparison (8,118 entries; 230,045,665 unpacked bytes). Bundled Python
derived tokenizer metadata from the cached target and validated the pinned
DFlash2 configuration without loading weights. The real CLI dry run selected
Splash as default alongside the llama.cpp alternative, credited shared target
weights, exposed draft/cache space, and created no root. Harmless native helper
processes proved three-role supervision and owned shutdown.

The authorized isolated native smoke passed on Apple M5 / 32 GiB with Splash
1.1.0 and llama-swap v257. It used the locked Unsloth UD-Q4_K_XL target, the
architecture-matched DFlash2 sidecar, Frog v22.5, int8 KV and a 24 GiB engine
ceiling. The loaded frontend reported 32,768 context tokens. With reasoning
disabled per request, a 128-token-capped chat returned exactly `SPLASH_OK` and a
second 128-token-capped request returned the required `report_status` tool call
with `{"status":"ok","count":2}`. Temper observed router, frontend and native
engine identities and proved all processes stopped and both listeners closed.
Repeated preparation preserved selection and generation. Public software removal
succeeded, and the isolated root and conversion cache were removed; the shared
Hugging Face cache remains available. These are installation
and short-request integration checks, not full-window capacity, default-medium
reasoning quality or performance qualification. No context finding was added.
The catalog now records 1.1.0 / v257 as tested software under these conditions.

Initial native attempts exposed a missing source-assembly `model.json` descriptor;
Splash consequently attempted its legacy packed-model path. The adapter now
writes and receipts the required descriptor, with a regression for its semantic
fields. Splash startup logs are forwarded through the router. The bundled
Transformers loader's generic Mistral heuristic is explicitly disabled for the
Qwen tokenizer already derived by Splash; the serialized Qwen tokenizer remains
unchanged. Failed attempts also ended with proved owned shutdown.

### Additional engine closures

The Qwen Field Kit revision 4 is the named consumer for Rapid MLX 0.15.2,
vLLM Metal 0.30.0 / vLLM 0.30.0+cpu, and an independent coding evaluator.
`catalog/experiments/qwen-study.json` freezes eight compositions. Exact Python
closures compile/render offline; both engine environments installed, passed pip
checks and accepted their CLI options on the development Mac. The evaluator
reproduced retained controls. The probe now binds the Python console process,
vLLM worker and tracker; Field Kit owns the two RAM routes and stop thresholds.

No weight download or model inference qualified these new routes. Native model
loading, readiness, filled context and completed-work performance on 36 GiB and
48 GiB+ machines remain pending. MLX-VLM and other topologies still need a named
consumer and exact closure; a reachable port alone is insufficient.

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
