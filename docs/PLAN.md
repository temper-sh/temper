# Temper — execution plan

Status: **ALPHA.11 PUBLISHED — GUIDED SETUP AND FIELD KIT HOST**

Updated: **2026-09-29**

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
| Maintained catalog | Artifact, Patch, Engine and Preset compile directly to execution-lock v3. [The execution-lock contract](contracts/execution-lock.md) owns current schema boundaries. |
| Guided setup | `init` and `configure` provide Presets (Recommended/All), arbitrary Layouts, then review/save/prepare. Revision-checked configuration is independent of managed activation. [The setup contract](contracts/init.md) owns current behavior; pre-preset configurations must be recreated. |
| Software selection | Source records are separate from resolved releases. `catalog compile --software recorded\|latest\|tested` supports the current llama.cpp/llama-swap macOS ARM64 sources. Recorded inputs are the offline default; fallback is explicit. |
| Installation | Exact isolated release/Python installation, receipts, version checks and recovery remain. Receipt reuse now compares installed software facts independently of execution provenance. Context and other runtime changes reuse unchanged software for llama.cpp and Splash. System-managed software is always retained. |
| Field Kit host | `catalog compile --preset` and `execution configure/inspect/prepare/render/paths/serve/remove` and supervised probes are implemented. The [runtime contract](contracts/execution-runtime.md) owns process identities, listener checks and final shutdown proof. |
| Current router compatibility | The runtime recognizes llama-swap's narrowly scoped macOS inspection helpers and Rapid 0.15.2's reviewed crash-log child, using kernel identity and ancestry. v260 passed native writing/context work. Engine lifetime remains fixed for measurements; unknown children still refuse ownership. Full Go tests, vet and race checks pass; failures and recovery remain in [Labs](../../v3/labs/workstreams/writing-and-orchestration/README.md). |
| Source and build | `08558c4` is published as `v0.1.0-alpha.11`, including guided setup, managed layouts, native Metal detection, Splash, reusable software receipts and the Qwen study host. Legacy schema removal is committed in `24df332`. |
| Public binary | Signed/notarized [0.1.0-alpha.11](https://github.com/temper-sh/temper/releases/tag/v0.1.0-alpha.11) passes downloaded checksum, signing identity, notarization and execution checks. Field Kit revision 4 pins this host; issued older studies keep their original hosts. |
| Catalog distribution | The live [stable channel](https://temper-sh.github.io/temper/catalog/channels/stable/channel.yaml) serves signed v3 sequence 3. Update, unchanged replay and authenticated compilation pass. [The catalog guide](CATALOG.md) owns use. |
| Published presets | Sequence 3 has five Recommended presets: Qwen/Splash, Glimmer, Gemma 26B, Gemma 31B and Qwen/llama. All retains Gemma E2B/E4B and Qwen3.5. The writing presets use b11205; Qwen/llama and smaller presets retain their exact closures. The router is v260. Historical evidence keeps its producing versions. |

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
At that point, these local compatibility fixes did not change the published
alpha.10/Field Kit alpha.9 binaries. Alpha.11 now includes them.

The catalog delivery verified the Pages-served bytes with the existing signing
trust root. A fresh update, unchanged replay, no-write dry run, offline selection
and compilation passed. The downloaded signed alpha.10 compiled byte-identical
inputs to the verified candidate. Hermetic tests cover interrupted staging,
signature failures, stale/equivocating publications, explicit rollback with a
retained highest sequence, writer contention and preserved user files. The
recorded Qwen inputs and unknown required/tested boundaries were not changed.

## Alpha schema cleanup

Current source accepts catalog v3, execution-lock v3 and manifest v2 only.
Removed Selection/Profile normalization, old setup pairs and wizard/import,
execution exports, the separate software catalog/updater and dead generic
resolvers/policy. Preset compilation is native. The proposed study patch is
integrated, including per-run configuration and v3 study authoring.

Historical context-evidence hashes retain their original definition. Published
snapshot bytes and historical measurements are unchanged. The cleanup is
committed as `24df332`. Issued experiments retain their pinned older host.

Verification passed: the full Go suite, vet and race checks; all eight study
presets compiled in a temporary Field Kit copy, whose verifier and 96 hermetic
tests passed. Its public inspect/configure integration accepted the new locks;
dry runs left no runtime root, and repeated configuration left output untouched.
The subsequent owner-authorized release preparation regenerates the current
Field Kit package with alpha.11; older packages and the live service are unchanged.

## Alpha.11 delivery

Tag `v0.1.0-alpha.11` publishes `08558c4`. Signed catalog sequence 3 contains the
exact guided catalog
snapshot `951f957516566a44d7c45e948b85f4ce10cda6c46be2fb1f09bc7aed74dc0d7d`.
Both signatures and their channel/snapshot join verify with the production key.
[Release notes](releases/0.1.0-alpha.11.md) cover the new operations and explicit
format break. The tag workflow requires those notes and a valid publication.

The tag workflow passed tests, signing and notarization and published both
release assets. The downloaded archive passes checksum, exact contents, file
modes, Developer ID, notarization and version checks. Its compiled inputs match
the candidate byte for byte for the same catalog source; authoring and signed
snapshot provenance retain the same execution identity. The live catalog passes
no-write update, fresh installation, unchanged replay and authenticated compilation.

Field Kit `515a9da` pins the actual signed archive checksum. Fresh software-only
setup and unchanged replay pass with the private Python 3.14.7 installation.
The default command reaches machine admission; no study session is created on
the ineligible development Mac. Older packages and unfinished runs remain unchanged.

The tagged source passes the full Go suite, vet and race checks. Building and
packaging twice leaves identical output. The cleanly extracted archive passes
version, shape, checksum, compile/configure/replay and execution dry-run checks.
Field Kit verifies and passes 97 tests on Python 3.9 and 3.14. Both synthetic RAM
routes pass signed-host preview without writes, and all 17 route cells pass exact
configuration and inspection. No study inference is implied by these checks.

## Next delivery

The Qwen study now authors from `temper-catalog/v3` and derives per-run limits
through `execution configure`, keeping the serialized lock opaque to Field Kit.
The legacy cleanup preserves these public preset operations. Configuration
returns the existing context-evidence identity consumed by wizard findings;
reviewed study results supply exact-machine context, per-role memory and latency
evidence. No result is automatically recommended or imported into the catalog.

**Preset selection and user-owned layouts — shipped in alpha.11; bounded
M5 native lifecycle checks completed with fixes.**

The [implementation plan](design/presets-and-layouts-plan.md) now has the five
Recommended presets and required authored copy, v3 authoring, a shared
Recommended/All selection, editable starter layouts and arbitrary compositions.
`temper-configuration/v1` stores exact reusable presets and layout references;
atomic revision checks protect edits and offline resume. The current binary
rejects retired formats; no importer is retained.

Preparation deduplicates weights and exact software closures. Managed rendering
composes Splash and different llama.cpp versions, separates startup/default,
and applies idle eviction. Explicit activate/status/stop reconciles a separate
activation journal and one owned launchd job. Durable engine launch identities
support cleanup after router exit; active requests, accepted connections and
uncertain ownership refuse transitions. No additional daemon or automatic
crash restart is installed. Fixed-process Field Kit supervision stays separate.

Gemma 31B was reconstructed from the retained QAT composition using
`compile-arm.py`. Its current medium-thinking default changes execution identity;
restoring the former off setting still reconstructs the historical digest.
Adding it did not run another model study.
Verification passed: full `go test ./...`, `go vet ./...`, `go test -race ./...`,
source build and whitespace checks. A real 120×40 terminal dry run covered shared
selection, renamed layout, separate startup/default, starter removal and review;
its root remained absent. A private-root CLI run covered save, two layouts sharing
one preset, offline resume, stale-edit refusal and read-only status. No model
process or launchd job ran.

The preset/layout editors retain the Tokyo Night palette, bordered cards and
download table. Larger main tabs have rounded borders and an open active edge.
Keyboard focus covers filters, each layout checkbox, contextual and navigation
buttons, form fields and review downloads. Up/Down moves between rows;
Left/Right moves within a row. Tab/Shift+Tab focuses forward/back actions;
Enter/Space activates them. Keyboard and mouse share the button definitions.
Narrow footers reveal the focused button instead of omitting contextual actions.
UI regressions cover navigation without letter shortcuts, empty filters,
resize to 28×10, independent switches, invalid input, preparation refusal and
cancelled previews. Full tests, vet and UI race checks pass. A 100×30 terminal
dry run completed preset selection, context editing, each layout checkbox,
idle-timeout editing, Downloads, Back and Save, leaving its root absent.

**Thinking controls, 2026-09-29 (owner).** Current authoring presets default to
medium; another level requires a test establishing the exception. Historical
off-only measurements do not establish that off is necessary. Wizard card
names and descriptions omit thinking settings. Issued catalogs, saved locks and
historical evidence retain their original settings. Pi exports allow request
overrides for llama.cpp and Splash, preserve explicit client preferences and
retain configured exceptions. Managed setup's Pi installation remains separate.
Full Go tests, vet and focused render/catalog race checks pass, and the source
binary is rebuilt. Pi 0.87.1 produced the expected 39 offline request payloads
across all eight current presets; all eight rendered wizard cards omit thinking
settings. This verifies client configuration and payloads, without new inference
or changed live services.

The authorized [bounded native smoke](design/managed-layout-smoke.md#observed-result)
completed mixed Splash/llama.cpp routing, independent startup/default selection,
idle unload/reload, busy-switch refusal, saved rename/deletion isolation and
owned shutdown. It exposed and fixed the v260 empty in-flight snapshot parser
and an `ESRCH` race during process observation. Glimmer supplied startup because
Splash's 24 GiB cap exceeded the 23 GiB startup allowance. Cold Splash conversion
exceeded the 60-second request bound; requests using its completed cache passed.
A closing connection briefly refused stop; explicit retry and an unchanged
second stop passed. Runtime stayed below seven minutes and output reservations
below 512 tokens. All private runtime material was removed after verified stop;
shared caches and the live stack were preserved. Full Go tests, vet and race
checks pass. Publication and release were subsequently authorized and completed
with alpha.11; live cutover remains separate.
Older setup implementation notes below retain historical evidence; current
behavior is defined by the command contract.

### Existing catalog work and implemented setup

The authorized 24 September overnight catalog review, bounded native
investigation and managed-serving/tool/integration research are complete.
The [workspace plan](../../v3/PLAN.md#full-stack-workstreams) retains all six
layers. Guided setup is implemented; the next work establishes practical
portfolio coverage, current-composition evidence, actual machine fit and the
tool/integration offering. Alpha.11 publishes the reviewed delivery.
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
absent. This historical check predates the preset editors shipped in alpha.11.

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

Field Kit revision 4 now uses signed alpha.11. Historical study results are
independent of this catalog work. Preserve their original package, producer,
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

### Role-specific S models

The [authorized M5 comparison](../../v3/labs/workstreams/writing-and-orchestration/README.md)
completed 80 requests across 36 configurations in 171.4 minutes of native runtime.
The S order is Qwen–Splash, Glimmer 30B, Gemma 26B-A4B, Qwen–llama. Glimmer uses
Q4 XL / official template / medium reasoning / F16 KV / no draft at 57,344
tokens. Gemma uses Unsloth QAT UD-Q4_K_XL / embedded template / thinking off /
F16 KV / no draft at 98,304. Both use b11205 and two context checkpoints.
Their exact context-execution identities survived editorial catalog naming.

Glimmer completed a conflict-recovery workflow but its technical plans still
needed material corrections. Gemma 26B delivered useful scenes and revisions
with much shorter waits than 31B. The
[initial record](../../v3/labs/workstreams/writing-and-orchestration/results/m5-2026-09-27.json)
preserves the Glimmer work, Google Gemma observations, larger-context timeouts
and runtime failures. The [QAT comparison](../../v3/labs/workstreams/writing-and-orchestration/results/qat-m5-2026-09-28.json)
adds 62 requests across 14 configurations in 62.8 native minutes. Its two extra
replacement controls remove downloader residency as a timing confound; all
original samples remain. Unsloth's 26B target is about 190 MB smaller and
reduced matched plain 512-token request time by about 5%. Writing outcomes were
mixed, not a demonstrated literary improvement.

The final plain 26B archive scene met its brief, but the laundromat scene
violated the closing deadline. The MTP variant additionally moved the shirt
home while negotiating immediate dryer use; it did not clear the frozen writing
criterion. Plain decoding is the conservative authored choice, not a claim
that speculation inherently reduces quality. The new 90,112-token input and
continuation passed in 554.25/4.96 seconds at 16.53 GiB peak engine RSS with no
new swap. The exact context-execution identity matches the compiled catalog.
Gemma 31B remains a documented Unsloth/plain/Q8/40k alternative, outside this
installable S selection.
The authored contexts reserve 4,096 output tokens rather than proving a full
output at those filled points. Public assessment/context URLs remain absent
until Results has a real published destination; setup requires explicit windows.

Exact GGUF draft support covers llama.cpp DFlash/DFlash2 and external MTP
assistants through ordinary preparation and supervised runtime. Regressions
cover pinned material, rendering, invalidation and refusals. The Rapid comparison
exposed its separate crash-log helper: the reviewed script digest, selected
interpreter and engine parent now bind that child to supervision. A native
request and shutdown passed. The original failure and verified recovery remain
in the research record. A separate transient llama.cpp shutdown refusal remains
unexplained; the error now reports its process row without weakening ownership.

Full Go tests, vet and race checks pass. Twenty repeated harmless native
supervision checks passed without reproducing that refusal. The rebuilt CLI's
M5 terminal preview shows all four S choices before XS with separate Model,
Weights and Engine labels. Scripted dry runs preserve explicit defaults and
independent contexts; both writing choices fit the current memory/disk preview.
The four-S-choice preview also compiles different engine versions and correctly
reports its fresh-install disk shortfall (49.50 GiB needed, 33.74 GiB available).
All preview roots remained absent. No live service or saved default was changed.

The QAT catalog revision passes the full Go suite and the two-writing-profile
dry run, with Gemma as the explicit default and independent contexts. Selecting
the research SSD through `HF_HUB_CACHE` reuses its downloaded target. Neither
preview creates its root. The final model cards, comparison and prepared Field
Kit questions retain quality limitations and the unqualified faster candidates.

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

Named-layout activation, status, stop and conservative switching are implemented
in source. See the current delivery and [operation contract](contracts/init.md).
Native acceptance, live service cutover, leases, automatic draining, force,
login startup and harness integration remain separate work. Existing user
manifests are not mechanically rewritten.

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
| Pure computation | Preset compilation, native rendering, diffs, budgets and installation plans. |
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
