# Presets, recommendations and user-owned layouts

Status: **IMPLEMENTED IN SOURCE; NATIVE MANAGED-MODEL ACCEPTANCE PENDING**

Decision date: **2026-09-28**

The owner requested implementation of this accepted handoff. This change
replaces the modes-first wizard with preset selection and layout composition,
and makes the curated preset list the main product value. This document records
the agreed direction and its implementation checks. The [current contract](../contracts/layouts.md)
and [setup guide](../contracts/init.md) define the implemented interfaces.
No live service change or publication is part of this source change.

Read the [repository instructions](../../AGENTS.md), [product spec](../SPEC.md)
and [execution plan](../PLAN.md) at implementation entry. The accepted decisions
below supersede conflicting older descriptions of the wizard, recommendation
policy and runtime vocabulary. Existing command contracts describe what runs
today until the implementation updates them.

## Accepted decisions

### Presets and layouts

- A **preset** is a particular model's weights, engine and settings, including
  relevant templates, draft models and inference controls. It is the unit we
  curate and recommend.
- A **layout** is a named composition of selected presets, with membership and
  loading choices. The same preset can belong to several layouts.
- **Local** and **Utility** are proposed starter layouts. Users can edit,
  rename or remove them and create their own. Their names are not runtime
  types or a closed list of allowed arrangements.
- Preset settings belong with the preset. Layout membership and startup loading
  belong with the layout. Selecting or installing several presets does not mean
  keeping all of them loaded.

The current single-configuration `Layout` maps to **Preset**. The current
composition `Profile` and user-facing mode concept map to **Layout**. Unify the
new vocabulary without introducing another synonymous public layer. Preserve
the meaning and identities of issued historical formats.

### Initial recommended set

There are **exactly five recommended presets for the initial change**:

| Preset | Existing implementation or retained starting point |
|---|---|
| Qwen / Splash | `qwen3.8-27b-q4xl-splash` in the authoring catalog |
| Qwen / llama.cpp | `qwen3.8-27b-q4xl-mtp` in the authoring catalog |
| Muse Glimmer 30B | `muse-glimmer-30b-q4xl-llama` in the authoring catalog |
| Gemma 4 26B-A4B | `gemma-4-26b-a4b-qat-ud-q4-k-xl-llama` in the authoring catalog |
| Gemma 4 31B | Retained `gemma31-ud-mtp0-final` composition; catalog addition required |

This table records membership, not a new ordering instruction. Keep the earlier
owner decision: largest available bucket first, Qwen / Splash first within S,
and the Qwen / llama.cpp baseline last. Suggested middle ordering is Glimmer,
Gemma 26B, then Gemma 31B; that placement remains an editorial choice.
Order changes never select a preset or change a user's default.

Existing smaller choices remain in **All**. Being outside Recommended does not
make a compatible preset unavailable. Recommendation membership is an editorial
decision, independent of speed rank or a synthetic pass/fail score.

Gemma's preferred weights are Unsloth QAT UD-Q4_K_XL. The retained 31B starting
point uses llama.cpp b11205, thinking off, plain decoding, Q8 KV, two context
checkpoints and a 40,960-token window. Exact files and controls come from the
retained record, not from reconstructing them from this prose. The existing
26B and Glimmer choices likewise remain starting points; this redesign does not
authorize another optimization sweep.

**Owner amendment, 2026-09-29:** use medium thinking as the default unless a
test establishes that another level is needed. Historical off-only runs are
not such a comparison. Current authoring adopts medium while retained results
and saved locks keep their exact settings. Remove thinking levels from wizard
cards and allow terminal-agent request overrides through the supported engine
controls; the preset supplies a default rather than disabling the selector.

### Required manual descriptions

Every recommended preset must have a non-empty, manually authored description.
It belongs to the **preset**, since the same model and weights can have different
engines, settings and reasons to choose them. A shared artifact description is
insufficient to distinguish Qwen / Splash from Qwen / llama.cpp.

The description should explain the useful applications and meaningful tradeoff
in ordinary language. Intended positioning is Qwen for coding, Glimmer for
technical writing, orchestration, planning and possible review, and Gemma for
creative writing and general chat. Retain relevant limitations without claiming
that these intended uses are all independently proved.

- Reject a recommended entry with missing or whitespace-only preset copy.
- Do not automatically fill the required description from specifications,
  model cards or benchmark summaries. A curator can deliberately reuse existing
  authored copy.
- Preserve authored wording through catalog, source and evidence updates.
- Allow descriptions for other presets without making them an All admission
  requirement. Assessment links are useful but not a prerequisite for writing
  the description.
- Descriptions, recommendation membership and ordering are presentation data;
  changing them must not change execution behavior or the user's selection.

The five descriptions still need review during implementation. Existing copy
can supply a starting point; the recommendation choice itself is already made.

### Catalog and evidence policy

Curated presets are the main value. Full local reproduction of every quality
claim is neither possible nor required. Accept trusted sources such as Unsloth
within their expertise, and weigh practitioner reports by background,
methodology and use case. Keep the source of a conclusion understandable without
creating another qualification registry.

Local checks establish the relevant installation, engine compatibility, memory,
speed and context behavior. Basic compatibility and speed testing can support
catalog inclusion without recommendation. A small local writing sample may
reveal limitations; it is not an automatic admission or recommendation gate.
Quality and usefulness take priority over raw throughput. Preserve historical
observations and their limits when changing the current editorial assessment.

**All** means all catalog presets, including Recommended. It does not imply an
exhaustive upstream model directory. Keep explicit/local configuration paths
available; lack of recommendation must not prohibit experimentation. A new
general model-import browser is not assumed by this plan.

## User flow and operations

The core wizard flow is **Presets → Layouts → Review and prepare**.

**Presets:** one screen with **Recommended** and **All** sub-tabs, sharing one
selection. Keep descending memory buckets, catalog ordering, machine-fit
information, download visibility and distinct Model / Weights / Engine details.
Selections survive tab changes and back navigation. Preset details expose
templates, context and other supported settings without repeating the picker
for every layout. Recommendations do not preselect models.

**Layouts:** begin with editable Local and Utility suggestions. Users choose a
name and membership from their selected presets and identify which load when
the layout activates. Include a separate default-model choice for clients:

| Choice | Meaning |
|---|---|
| Included | Available in this layout; can load when requested |
| Load on activation | Start loading when this layout activates; several are possible when their combined memory fits |
| Default model | The included preset used by the layout's default route; independent of startup loading |

A helper-only layout can have no default local model. A default or startup
selection must reference an included preset. Removing one must make the needed
correction visible instead of silently choosing a replacement. Startup loading
must not silently acquire a permanent keep-loaded policy; decide that behavior
explicitly with the runtime work below.

**Review:** show preset installation/download costs once across the selection,
then each layout's membership, startup set, optional default and relevant memory
estimate. Distinguish available presets from simultaneous residency. Save and
prepare remain separate from activation.

After setup, reuse these editors for preset and layout management. Support
activating a named layout, switching layouts, inspecting status and stopping.
Deleting a layout removes its composition, not shared preset files. Referenced
preset removal must identify affected layouts and require an explicit resolution.
Saving an edit does not silently change the active runtime.

## Implementation baseline and original gaps

This table records the baseline before implementation. Current behavior is
defined by the [setup contract](../contracts/init.md) and
[execution-lock contract](../contracts/execution-lock.md).

| Current surface | Change needed |
|---|---|
| [Setup command](../../internal/setupcmd/command.go) and [TUI](../../internal/setupui/setupui.go) | Local/Utility are hardcoded selection paths; installation and default choice are coupled to local profiles. Replace with one preset selection and user-owned layouts. |
| [Catalog records](../../internal/catalog/catalog.go), [presentation](../../internal/catalog/presentation.go) and [description editing](../../internal/catalog/presets.go) | Descriptions belong to artifacts in the baseline; there is ordering but no recommended/all distinction. Add preset editorial data and the mandatory-description rule. |
| [Setup planning](../../internal/setup/plan.go) and [storage](../../internal/setup/store.go) | Saved pairs and defaults rely on local/utility names. Preparation refuses a profile with more than one current Layout. Support arbitrary named compositions and shared presets. |
| [Catalog compilation](../../internal/catalog/compile.go) and [rendering](../../internal/render/render.go) | Catalog validation currently rejects conflicting engine closures in a profile. Qwen/Splash plus llama.cpp, including different llama.cpp versions, must compose without collapsing their exact dependencies. |
| [Execution runtime](../contracts/execution-runtime.md) | The measurement supervisor expects a fixed engine lifetime. Managed availability, idle unload/reload and layout transitions need their own lifecycle behavior. Preserve Field Kit's measurement contract. |

Sources to reuse:

- [Current authoring catalog](../../catalog/guided-setup.json) and its
  [guide](../../catalog/README.md).
- [QAT result](../../../v3/labs/workstreams/writing-and-orchestration/results/qat-m5-2026-09-28.json),
  configuration `gemma31-ud-mtp0-final`, and the existing
  [compile-only reconstruction helper](../../../v3/labs/workstreams/writing-and-orchestration/method/compile-arm.py).
  Supply this result explicitly with `--result`; the helper otherwise uses the
  earlier study. It performs no inference or downloads.
- [Gemma 31B assessment](../../../v3/results/models/gemma-4-31b/README.md) and
  [writing investigation](../../../v3/labs/workstreams/writing-and-orchestration/README.md).
  Their former “alternative” framing does not override the saved recommendation
  decision. Their measurements remain historical evidence.
- [Managed serving findings](../../../v3/labs/workstreams/managed-tools/README.md)
  for start/stop/status, ownership, interruption and request handling.

## Implementation sequence

### Define the new contracts and persistence

Update the vocabulary and affected contracts before wiring screens. Specify the
smallest representation for catalog presets, recommendation metadata, selected
presets and user-owned layouts. Reuse existing bindings where they express the
new behavior; do not create a separate mode registry or duplicate artifact facts.

Separate configuration editing from observed activation state. Define explicit
save/update behavior for user choices, atomic publication, interrupted-save
recovery and concurrent edits. Settle schema versioning and the transition from
saved `local`/`utility` pairs without overwriting user edits. Issued catalogs,
locks and Field Kit inputs retain their historical identities. Do not add broad
compatibility machinery for formats with no actual consumer.

### Add catalog recommendation data and the fifth preset

Add the five recommendation memberships and preset descriptions; update the
existing description-editing route to address preset copy. Carry over compatible
existing manual wording deliberately. Add Gemma 31B from its retained exact
composition, with the recorded Unsloth material and measured context point.
Update the authoring catalog and its explanation together. Keep existing All
choices and do not rerun unchanged model research merely to add metadata.

### Compile and prepare reusable presets and composed layouts

Remove the single-preset preparation restriction through real composition
support. Resolve dependencies per selected preset/engine version, deduplicate
shared files and software only when identities match, and retain exact rendering
per preset. Mixed Splash/llama.cpp layouts are a required case.

Calculate disk cost over installed material and memory over the relevant active
set. Validate default/startup membership, duplicate references and incompatible
simultaneous loading. Preserve usable dry runs and offline resume. Preparing
multiple alternatives must not imply that their combined runtime memory is
required simultaneously.

### Build the two editors and scripted equivalents

Replace modes-first navigation with Presets and Layouts, followed by combined
review. Keep the existing Bubble Tea components and useful navigation. Both tabs
must operate on the same selected set. Layouts reference that set without
reinstalling or copying presets per layout.

Expose the same decisions through CLI/JSON operations so agents and scripts can
create, inspect and edit named layouts. Select command names as part of the
contract work; this plan does not freeze a speculative command inventory.
Reuse these operations after initial setup rather than making `init` the only
way to manage choices.

### Implement managed activation and transitions

Keep managed operation separate from the fixed-process experiment supervisor.
Use the existing owned-process and rendering primitives; do not add a new
background orchestration daemon. The first implementation can use one active
layout at a time, with arbitrary saved layouts.

Validate prepared inputs and memory before switching. Reconcile desired and
observed state, preserve reusable running presets where safe, and unload before
loading where coexistence does not fit. Idle eviction and later demand must be
normal managed behavior. Report partial failure accurately and make retry/stop
converge without orphaned processes or signaling unrelated ones.

Use the managed-serving study's conservative starting policy: refuse a switch
that would interrupt active work. Automatic draining, cross-harness leases and
force behavior are separate decisions, not implied by this wizard redesign.
Deleting or editing an active layout must not silently terminate work.

### Verify and update the maintained surfaces

Test the meaningful boundaries listed below, then run the repository's required
Go tests, vet and race checks for the changed product code. Exercise a real
terminal dry run and scripted save/resume with private roots. Update the setup,
catalog, execution and operation contracts, README, relevant V3 requirements and
Results terminology. Preserve old evidence wording where it names historical
formats or measured inputs; revise current recommendations in place.

Reuse applicable native evidence. If the new lifecycle requires native model
validation, prepare one bounded smoke with memory preflight, exact processes,
requests and cleanup; obtain the authorization required by repository rules.
Service installation on the user's live stack, publication and release remain
separate from implementation in an isolated root.

## Decisions to settle during implementation

The recommendation roster, manual-description requirement, two screens and
editable starter layouts are settled. The following mechanics remain open:

- How a customized preset is saved and named, and how edits to a preset shared
  by several layouts expose their impact. Do not silently claim exact catalog
  evidence for changed settings.
- The distinction between preload, idle timeout and keeping a preset resident;
  how competing on-demand requests are handled when both models cannot fit.
- The smallest persistent activation mechanism and handling of an active layout
  being renamed/deleted. The existing managed-serving study is the starting
  point, not authority to install a live service now.
- The exact schema/save transition and CLI names. Prefer routine engineering
  judgment; ask the owner only if a choice changes the agreed user behavior.

## Completion checks

- Recommended contains the five saved choices; All includes them and existing
  compatible non-recommended presets. Every recommended preset has manual copy.
- Blank required copy is rejected. Editorial changes preserve execution
  identity and existing user selections.
- Tab switches and back navigation retain one selection; grouping and editorial
  order remain correct without selecting a default on the user's behalf.
- Users can remove the proposed Local/Utility layouts, create their own, reuse a
  preset across layouts and choose inclusion, startup loading and a default
  independently. Helper-only layouts need no default local model.
- Mixed engines and versions prepare/render correctly; shared material is
  counted once. Membership does not imply simultaneous residency.
- Save/cancel/dry-run, interrupted writes, concurrent edits, offline resume and
  repeat operations preserve user state. Removing a layout preserves presets.
- Managed demand loading, idle unload/reload, stop and safe switching work with
  accurate desired/observed status, bounded recovery and verified ownership.
- Issued experiment inputs and their fixed-lifetime supervision still work.
  Historical evidence is retained without turning its small quality screens
  into new catalog gates.

## Implementation verification and remaining boundary

Source implements the catalog, persistence, editors, shared preparation and
managed lifecycle. Gemma 31B matches the retained native execution digest;
existing dirty engine/catalog work and historical experiment formats are
preserved. Configuration and lifecycle regression tests include interrupted
saves/effects, stale writers, referenced removal, independent defaults/startup,
mixed closures, PID reuse and orphan recovery. A disposable-process test covers
the durable launcher and later reload without a model or launchd.

The new native model/launchd behavior still requires the separately authorized
[bounded smoke](managed-layout-smoke.md). Hermetic checks do not claim runtime
qualification. No model download, inference, launchd change, live cutover,
commit, publication or release is authorized by this verification note.
