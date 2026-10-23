# Craft skills — real-work field notes

Temper is the craft set's first sustained real-product test. These notes close
the execution plan's craft-skill field-test objective: after each named
workstream through the 1.0 release, record how the skills affected actual work
and propose a narrow
improvement only when the phase supplies evidence for it.

This is not a model-eval log, product evidence, or permission to change the
skills repository. It creates no synthetic Temper work. A note may — and often
should — conclude that no skill change is warranted.

## Wizard context prompt, 2026-10-23

Unit-design guidance kept pending context text inside the restored prompt;
typing does not change the selected preset or its memory assessment. Apply
reuses the existing validation and assessment boundary. Regression stories cover
Apply, Cancel/Escape, invalid input, narrow-terminal navigation, mouse actions
and preservation of inline Compute choices. Publication and schemas are unchanged;
existing guidance was sufficient, with no skill change warranted.

## Visible forms and local-template import, 2026-10-23

Unit design kept raw input, validation errors and pending attachments with each
tool's draft; shared `tui` controls only render them. Cursor movement and drawing
must not author an override. Data modeling distinguished an inherited seed from
explicit Random (`null`) and Fixed zero; named modes retain the existing numeric
meanings for limits, cache and engine-default thread counts.

Reliable-effects guidance put local-template selection before publication:
selection snapshots bounded bytes, review resolves that snapshot, and explicit
Save syncs managed copies before publishing the preset reference. Failure cleanup
removes only files created by that save; retries reuse matching prepared copies.
Restoring a missing attachment preserves an unchanged preset's bytes and mtime.

[Preset form stories](../internal/presetcmd/editor_form_test.go),
[layout settings stories](../internal/setupui/layouts_form_test.go) and
[wizard controls](../internal/setupui/presets_navigation_test.go) exercise actual
typing, choices, invalid drafts, conditional settings and navigation. Existing
source/compiler checks remain in place. [Import stories](../internal/userpreset/template_import_test.go)
cover dry runs, cancellation between attachment and preset publication, collision
refusal, snapshot identity, cleanup ownership and clean retry. Full tests, vet and
race checks pass; actual renders cover 40/80/112 columns, 24/32 rows and both
true-color and 256-color terminals. A 40-column terminal run verified Fork/name,
explicit dry-run validation and cancellation with the authoring root absent.
**No skill change warranted.**

## Typed preset inputs

Unit-design guidance kept engine choices in the existing catalog field metadata
and reused shared button geometry for rendering and clicks. The editor owns the
pending selection and its Apply/Cancel boundary. Tests reproduced the free-text
problem, then exercised engine-specific KV values, booleans, omitted values,
wrapped mouse targets, resize, cancellation and explicit saves. Existing
reasoning tests now select options through the UI while retaining their coupled
parser/engine assertions. No skill change is proposed.

## Layout composition checkboxes, 2026-10-22

The owner's clarification identified the former wizard's inline composition
controls as the intended surface. Compact cards reuse `optionCard` and
`tui.ActionsWithTargets`; the layout model owns selection, guarded toggles and
saving. No new preset or configuration abstraction was needed.

[Card stories](../internal/setupui/layouts_cards_test.go) cover arrow-only
composition, disabled choices, default replacement/removal, guarded exclusion,
wrapped/scrolled mouse targets and unchanged exact presets. Existing
[mouse stories](../internal/setupui/layouts_mouse_test.go) retain pending-save
and confirmation checks. Full tests, vet and race tests pass. Actual renders at
40/80/112 columns and a 40-column terminal dry run covered checkboxes, review,
dry-run Save and discard; saved configuration stayed unchanged. Existing
source/revision checks retain the unchanged publication-boundary evidence.
**No skill change warranted.**

## Shared terminal keyboard behavior, 2026-10-22

Three real consumers justified extracting `tui.FocusOrder`: a pure region/row
transition that leaves scrolling, drafts and activation with each tool. Shared
choice cycling and focus hints keep the wizard and both editors aligned.
Inputs use Cancel/Apply and return to content; preset confirmations stay local,
and read-only Review no longer saves when Enter has no focused action.

[Focus tests](../internal/tui/focus_test.go) cover traversal and empty regions;
[layout stories](../internal/setupui/layouts_navigation_test.go),
[wizard stories](../internal/setupui/presets_navigation_test.go) and
[preset stories](../internal/presetcmd/editor_arrows_test.go) cover user outcomes,
validation, unavailable choices, confirmation and explicit save. Regression
cases first reproduced the layout/input and preset Review/confirmation gaps.
Full tests, vet and race tests pass. All three 40-column terminal dry runs
completed without changing authoring/configuration/harness state; current
40/80/112-column galleries are refreshed. Existing effect-boundary tests retain
conflict, interruption and rerun evidence. **No skill change warranted.**

## Preset arrow navigation, 2026-10-22

The owner clarified that Tab reached the buttons, but arrow navigation was
inconsistent with the wizard. Navigation now owns movement across content,
breadcrumbs, sections and actions; mode handlers keep their existing guards.
Shared rendering stays in `tui`; tool-specific focus remains in the editor.

[Arrow stories](../internal/presetcmd/editor_arrows_test.go) first reproduced
the trapped focus, then covered Fork/Copy through explicit Save, scroll return,
inline validation/cancellation, empty/disabled choices, source forms and file
selection. Full tests, vet and race tests pass. A 40-column terminal dry run
reached Copy/Cancel, Fork/name Apply and Validate without Tab; Ctrl-C left the
authoring root absent. Existing publication tests retain conflict, interruption
and clean-rerun evidence. **No skill change warranted.**

## Catalog preset overview and authoring actions, 2026-10-22

Unit design kept the read-only overview separate from draft editing and reused
the shared field renderer. The viewed source stays explicit through scrolling
and name-prompt cancellation; Fork/Copy enter the existing draft flow and Save
retains publication authority. Structured memory rows project `setup.Assess`
without parsing its command-line prose or changing admission rules.

[Overview stories](../internal/presetcmd/editor_overview_test.go) reproduced the
missing description/actions, then exercised mouse and keyboard creation through
Save, cancellation, unavailable sources, memory refusals and missing facts.
Measured-memory cases retain loaded/peak values, counter, context, workload,
machine, date and limitations. Full offline tests, vet and race tests pass;
40/80/112-column renders and a 40-column terminal dry run cover the new page.
Copy cancellation, Fork, Validate and Ctrl-C left the authoring root absent.
Existing publication tests retain conflict and clean-rerun coverage.
**No skill change warranted.**

## Inline preset content, 2026-10-22

Unit design kept the shared `tui.FieldRow` a renderer of supplied values;
preset settings and source forms reuse it while the editor retains focus,
pending input, validation and guarded Save. The
[interaction contract](contracts/user-presets.md#interactive-authoring) separates
Apply into the draft from publication and keeps other controls inactive during
an edit.

[Editor stories](../internal/presetcmd/editor_content_test.go) first reproduced
the lost field context, then covered inline errors, keyboard and mouse
Apply/Cancel, retained Unicode input across resize and explicit Save.
[Renderer coverage](../internal/tui/field_test.go) catches width overflow and
ANSI styles becoming visible text: underline belongs on the input's plain text,
before rendering its cursor and colors.

Full offline tests, vet and race tests pass. Actual renders cover 40/80/112
columns, 24/32 rows and truecolor/256 palettes. A 40-column terminal mouse and
keyboard dry run exercised validation, correction, Apply, Cancel and Validate;
Ctrl-C left the authoring root absent. Existing publication tests still cover
cancellation, conflicts and clean reruns. **No skill change warranted.**

## Mouse interaction in both editors, 2026-10-22

The owner found that the preset and layout editors rendered controls without
enabling mouse input. Earlier render and keyboard checks did not establish
click behavior. [Preset stories](../internal/presetcmd/editor_mouse_test.go) and
[layout stories](../internal/setupui/layouts_mouse_test.go) reproduced that
failure before the fix, then exercised edits through guarded saves, cancellation,
dependency confirmations and pending work.

Code organization and unit design kept hit testing and clipped frame placement
in `tui`, while each editor retains its draft and existing action guards.
[Frame tests](../internal/tui/mouse_test.go) check visible cells against actual
rendering; editor stories cover scrolling, wrapped rows and stale targets after
resize. The layout viewport now scrolls without snapping back to its selection.

Full offline Go tests, vet and race tests pass. Actual 40-column terminal mouse
checks exercised navigation, fields, membership, confirmations and dry-run saves;
the preset root stayed absent and saved layout configuration stayed byte-identical.
Unchanged publication boundaries reuse their existing failure and clean-rerun
tests. **No skill change warranted.**

## Three-step wizard and shared terminal layout, 2026-10-22

Code organization reused the frame, navigation, actions, input and palette
already consumed by the preset and layout editors. Mouse targets now come from
the same renderers as their labels; frame offsets keep scrolling and wrapped
footers aligned. The wizard retains its own revocable draft and guarded page
transitions. Layout editing remains with the standalone editor.

The contract distinguishes a first-setup composition from persistent automatic
membership: first setup rebuilds Default from current selections before review;
reruns preserve existing layouts, including the absence of Default. Referenced
preset removal is refused with an actionable route to `temper layout`.
[Wizard stories](../internal/setupui/presets_test.go) cover these outcomes, while
[navigation and view tests](../internal/setupui/presets_view_test.go) cover
stale previews, field cancellation, narrow controls and mouse geometry.

Review now confirms selections and resource cost without repeating settings or
paths. Its file table is always visible and scrolls inside the shared frame;
the obsolete disclosure state and controls were removed. The
[review tests](../internal/setupui/presets_review_test.go) retain separate disk
budgets, installation refusals and measured-memory counters, workloads and limits.

Full offline Go tests, vet and race tests pass. Actual terminal renders cover
40/80/112 columns, 24/32 rows and true-color/256-color profiles. Isolated fresh
and saved-root terminal dry runs verified context edits, Back/Next, harness
consent, removal refusal and Save without changing root/configuration/harness
files. Existing galleries and working-tree changes were preserved.
**No skill change warranted.**

## Fork update reporting, 2026-10-22

The resolved execution lock cannot recover an overridden origin value. Data
modeling placed a compact historical baseline beside the saved preset, retaining
only overridden catalog fields and distinguishing untracked from unset and zero.
This makes comparison independent of a separately refreshed catalog. Unit design
keeps capture/comparison pure in `setup`; commands own reads and publication.
Reliable effects advances the baseline at the configuration commit, preserves it
on dry/software-only updates and refusals, and returns the report even if later
preparation fails. A comparison-only save has no installation or restart effects.

[Pure comparison tests](../internal/setup/fork_updates_test.go) cover selected
profiles, equal-to-origin overrides, absent/zero/false values and source changes.
[Command stories](../internal/setupcmd/update_overrides_test.go) exercise first
layout placement through reporting, interruption, concurrent edits, rejected
rebuilds, unknown history, override removal and post-commit failure. Full offline
Go tests, vet and affected-package race checks pass. Existing UI evidence remains
applicable; no visual behavior changed. **No skill change warranted.**

## Layout composition and preset-folder discovery, 2026-10-22

The owner separated authoring from composition. The first implementation
incorrectly required a separate configuration selection before a saved preset
could appear in a layout. The corrected flow reads `presets.d` directly.
Unit design keeps the save callback limited to membership/settings; its command
captures newly included exact presets in the same configuration commit. Data
modeling retains the authoring source ID so updates cannot reinterpret a local
preset as a catalog entry. Code organization reuses the shared terminal frame,
existing file loader/compiler and layout memory assessment.

Reliable effects reused the configuration writer's revision guard and atomic
publication. [Command stories](../internal/setupcmd/layout_editor_test.go) prove
preservation of exact locks, integrations, unrelated layouts and the running
generation, plus conflict refusal, dry runs, cancellation and clean repeats.
[UI stories](../internal/setupui/layouts_test.go) cover revocable navigation,
dependent-reference clearing, generated names and post-publication errors.
[Folder integration stories](../internal/setupcmd/layout_presets_test.go) follow
preset-editor Save through first layout placement, invalid files and changed
source/template refusal. [Update stories](../internal/setupcmd/update_test.go)
cover fork/copy ownership, catalog ID collisions and invalid/missing-file lock
retention. Source identity survives wizard edits and named layout references.
Full offline tests, vet and affected-package race checks pass; actual narrow/wide
renders and an isolated terminal dry run cover the UI. This is composition and
save evidence, not native model or activation evidence. **No skill change warranted.**

## User choices instead of a schema editor, 2026-10-22

The owner rejected an Advanced category: fields must represent useful choices
the user can make. Unit design kept that UI projection in `presetcmd`, separate
from the complete file/command field table. Technical values remain preserved;
the UI derives IDs and formats, omits fixed or inapplicable controls and groups
dependent speculation/reasoning values behind their meaningful choice. Runtime
tuning remains in ordinary sections.

[Editor stories](../internal/presetcmd/editor_fields_test.go) cover generated-name
collisions, non-Latin names, omitted reranker/Splash controls, valid speculation
off/on changes, preserved Python compatibility metadata and Rapid reasoning
across saves. These checks exercise authoring outcomes rather than field counts.
Full offline Go tests, vet, affected-package race checks and actual narrow/wide
renders cover the delivery. **No skill change warranted.**

## Preset information reduction correction, 2026-10-22

The first shared-component delivery deferred field reduction despite the prior
owner decision and added a full-help screen. The owner corrected both. The
editor now uses contextual footer hints, source-specific forms, generated
material IDs, read-only provenance and markers only for explicit overrides.
Revision, format and transform remain available under source options.

Data modeling's partial-data guidance kept absent source license metadata
explicit as “Not declared by source” in local authoring; it is neither a
permission claim nor an import refusal. [Source-switching and missing-metadata
tests](../internal/presetcmd/editor_material_test.go) exercise the actual editor
through Save, retaining the draft boundary.
Full offline Go tests, vet and affected-package race checks pass; actual renders
cover narrow forms and both color profiles. This was a failure to apply settled
scope, not a missing craft rule. **No skill change warranted.**

## Shared terminal components, 2026-10-22

The accepted HTML study now supplies the shared frame, palettes, breadcrumbs,
secondary navigation, buttons and help in `internal/tui`. Code organization
kept these renderers in the existing UI package, with the preset manager/editor
as the first consumer. Unit design kept focus, editing and guarded actions with
the tool; value inputs describe what to render without a shared flow engine.

Outcome tests caught a long file path hiding a save-conflict reason and narrow
breadcrumbs truncating a short current location unnecessarily. Navigation
checks preserve drafts, invoke the existing save boundary and restore the
previous section when returning from a nested form. Actual renders cover
40/80/112 columns, 24/32 rows and true-color/256-color profiles. An isolated
terminal dry run exercised validation, help and the unsaved-draft guard while
leaving its authoring root absent. Full offline Go tests, vet and affected-package
race checks pass. Content and remaining action design, plus the layout editor's
interaction/save contract, remain open. **No skill change warranted.**

## Shared terminal visual design, 2026-10-21

The first visual draft explored a shared
[element/state color table](../internal/tui/theme.go) across the two working
TUIs. Code organization kept colors and text input together while leaving
navigation and geometry with each screen. Unit design made focus and selection
independent authored states. The owner retained the scratch direction but
found that it reduced accents too much, and clarified that the design must
also cover the separate layout editor before changing the wizard.

The wizard's original appearance is restored, with its original colors supplied
to the shared input helper. All 54 retained wizard reference frames match
exactly; the first scratch gallery is unchanged. Further exploration uses
isolated view overrides and proposed layout renderers in scratch. This keeps
visual exploration separate from product implementation; a proposed layout
screen does not settle its interaction/save contract.

Actual renderer output exposed both excessive inheritance lines in the editor
and unstyled spaces interrupting a focused background. Wide fields now align
labels, values and inheritance; narrow fields retain all three in stacked rows.
The local before/after gallery covers 112/80/40 columns and true-color/256-color
profiles, including loading, input errors and failed previews. Existing
navigation, license-link, cancellation, save and viewport tests remain applicable.
Full Go tests, vet and affected-package race checks passed, including after
restoring the wizard. The three-tool design remains under review;
**no skill change warranted**.

## Revocable preset authoring, 2026-10-21

Unit design kept the editor's draft separate from selected execution locks and
layout membership. Code organization reused catalog field descriptions, the
compiler, memory assessment and installation planner; the scripted commands
and editor share metadata reads, and setup shares the text input control.
These existing units supplied the authoring workflow without a new preset
schema or layout-specific settings mechanism.

Reliable effects placed publication in `userpreset`: a byte comparison, a
short writer lock and one staged atomic publication. The
[effect-boundary tests](../internal/userpreset/store_test.go) cover dry runs,
cancellation, stale files, competing writers, clean repeats and deletion that
preserves shared material. The documented external-editor race remains an
optimistic-edit limitation; the lock serializes Temper writers.

[Editor stories](../internal/presetcmd/editor_test.go) verify real key messages,
draft preservation, reset/conversion, pinned metadata, cancelled reads and
unchanged configuration/locks. They caught the file picker's incorrect Space
key name. Actual renders at 112/80/40 columns exposed mid-word wrapping, which
was corrected; an isolated PTY dry run left its root absent. Full offline Go
tests, vet and race checks passed. This verifies authoring, not model behavior
or the remaining layout integration. **No skill change warranted.**

## Specialized tools enable richer composition, 2026-10-21

**Owner observation during the 0.4.0 editor design:** narrow, specialized tools
and meaningful abstractions can later combine into something more powerful
than keeping their responsibilities together. Separation creates opportunities
for new uses as well as making existing work easier to understand.

Presets describe model configurations; layouts compose them for particular
work. The proposed preset editor owns authoring a variant, while the layout
editor owns the explicit decision to use it: add it, replace another preset,
or build a new composition. The wizard combines those capabilities into a
simple initial setup. Users can move from that guided path to the specialized
tools as their needs grow. Saving a custom preset does not silently place it
in a layout.

The discussion initially missed an abstraction already present:
[user presets](contracts/user-presets.md#two-forms-fork-and-copy) express
customization through forks (catalog origin plus overrides) and independent
copies. They can supply different model settings to different layouts without
inventing another mechanism for overriding preset settings inside a layout.
This is a missed opportunity to reuse an existing abstraction.

The owner suggested a possible motivation: avoiding many nearly identical
presets in the UI. That usability concern is a hypothesis about the earlier
reasoning. A later integration convention could hide presets created for a
layout from general pickers while exposing them where they are used. Such a
convention can smooth the composed workflow without changing what a preset
means; its implementation is not selected here. Settings unrelated to a preset,
such as layout-specific harness policy, can still have their own home outside
the preset.

**Craft handoff:** retain this as candidate emphasis for `code-organization`
and `unit-design`: assess a boundary by the useful combinations it enables,
alongside responsibility and dependency isolation. Check whether existing
abstractions already express the customization before adding another settings
mechanism; integration conventions can address presentation concerns while
unrelated policies retain separate ownership. This is an owner-led design
observation; the editor flows and their usability remain to be implemented and
verified. **Disposition: recorded for Craft review; skills unchanged.**

## Native reasoning budgets and Gemma adoption, 2026-10-20

Data modeling kept omission, unrestricted thinking (`-1`) and immediate end
(`0`) distinct, and separated thinking tokens from effort and total output.
Review of the pinned native parser exposed a non-obvious override trap: a
request's `-1` falls back to the server budget. Rendering keeps that server
unrestricted and uses a router default on the lower-priority native alias,
preserving callers using either spelling. The
[contract](contracts/execution-lock.md#settings-and-evidence) records the upstream
semantics; [tests](../internal/catalog/reasoning_budget_test.go) verify the
resulting filter and command contract through catalog, lock, manifest and fork
round trips without pretending to run inference.
Validation also bounds the value to the native signed 32-bit counter; the
largest value and the first overflow are covered at the serialization boundary.

The two Gemma memory snapshots were built from the retained tested executions,
then compared with the proposed presets. A separate engine pin kept the four
existing observations applicable. The larger thinking-off capacity peaks remain
paired with their own loaded samples and workload limits; lower short writing
peaks do not replace them. Full offline Go tests, vet, affected-package race
checks, all catalog assessments and a mixed-version layout render passed.
Existing guidance was sufficient; **no skill change warranted**.

## Pi/PiG compaction targets, 2026-10-20

Data modeling separated a chosen compaction target from actual model context,
and retained each reviewed settings destination. Reliable effects placed
ownership beside the token fields so clearing a target needed no separate
receipt or crash reconciliation. Settings publication followed successful
provider publication; ordinary activation repaired a partial file update.

The compaction reconciliation tests covered preservation, unowned collisions,
shared destinations, cleanup and clean reruns. Activation tests verified
recovery without restarting an unchanged service; wizard tests verified
explicit, revocable and shared choices. Full Go tests, vet and affected-package
race checks passed. Native compaction quality and latency remained unmeasured.
**No skill change warranted.**

**Disposition:** the owner subsequently withdrew **Compact around** and asked
to retain these Craft observations. Its control, saved fields, settings
publication and dedicated tests were removed. Live client files had not been
changed. Research and a deferred design question about layout-level policy
remain in the workspace backlog. Passing implementation checks did not settle
the appropriate policy scope. The retained model integrations pass full Go
tests, vet and affected-package race checks after rollback.

## Hermes integration, 2026-10-20

Reliable effects kept the existing preflight/publication boundary while adding
a YAML projection. Data modeling kept the visible provider name `temper`,
root ownership and the saved profile destination separate. Checking upstream
showed that unknown provider options produce warnings, so ownership lives in
a provider-local comment; automatic discovery remains enabled by owner decision.

The [adapter tests](../internal/harness/hermes_test.go) cover preservation,
ownership conflicts and repeated activation. A shared
[activation test](../internal/setupcmd/harness_test.go) caught invalid YAML
when an ownership comment was inserted into a compact mapping. Managed
mappings now use block form and the rendered document is validated before
publication. Full Go tests, vet and affected-package race tests pass.
Native Hermes/model execution remains untested. **No skill change warranted.**

## Harness synchronization and memory accounting, 2026-10-20

Data modeling separated the selected harness destination from environment-based
discovery, and artifact geometry from executable identity and runtime
measurements. Reliable effects made service readiness and each external file
separate outcomes: a file failure stays visible and a retry repairs it without
restarting a healthy service. Shared destinations are merged once; unrelated
settings and authentication remain owned by the user.

Review of Pi's picker and footer exposed a missed public surface: both display
the provider key directly. The owner selected `temper`; root ownership remains
separate and refuses another root's takeover. The [harness contract](contracts/harnesses.md)
and collision tests now reflect that decision. This was a missed application
of surface-first guidance, not a need for another skill rule.

Testing kept arithmetic, metadata parsing, wizard edits and activation recovery
at their respective boundaries. It caught a stale memory amount on wizard cards
after context edits. The [harness](../internal/harness/harness_test.go),
[activation](../internal/setupcmd/harness_test.go) and
[memory](../internal/setup/memory_estimate_test.go) tests cover preservation,
concurrent writers, cancellation, clean reruns, missing geometry and overlapping
allocation envelopes. Full Go tests, vet and race checks pass; native client and
model measurements remain separate. **No skill change warranted.**

The owner's Splash wizard report then exposed a wrong oracle in the passing
tests: the 24 GiB native ceiling had been treated as a required allocation.
Retained 1.3.2 evidence instead measured 19.46 GiB loaded / 20.46 GiB peak engine
footprint during short requests at a 112k allocation. Data modeling placed the
reviewed measurement on the catalog profile with its exact tested execution,
machine, counter and workload. Runtime edits invalidate applicability without
rewriting historical evidence; missing measurements use estimation.

Testing now checks that matching measurements replace estimates, edited
executions fall back, and an allocation ceiling equal to the Metal budget does
not itself refuse Splash. The audit also separated mapped-memory counter
blind spots from genuinely low use, and earlier-engine measurements from current
catalog facts. The research protocol owns future measurement recording and
catalog handoff. These were missed applications of evidence and oracle guidance,
not reasons to add a new Craft rule.

## First public schema reset and release preparation, 2026-10-04

Data modeling kept the current v1 contracts separate from immutable historical
publications and measurements. Unit design and testing guided removal of
unreachable APIs while retaining current recovery and failure checks. Synthetic
mixed-engine fixtures replaced tests pinned to a historical catalog snapshot.
The release verifier now checks signed bytes against the authored source.

The full Go suite, race checks, vet, deadcode and Staticcheck U1000 pass. No new
model inference was needed for these code and packaging changes. The authorized
catalog signing used the retained stdin-only tool: the staged publication
verified before replacing the local pointer, and exact source equality verified
afterward. Publication remains separate. **No skill change warranted.**

## Model license visibility, 2026-10-04

Testing focused on the user's actual choice: the license comes from the saved
artifact even when the current catalog differs, and its hyperlink survives
both category navigation and responsive review tables. The narrow review check
caught a split license identifier; placing the license on its own line kept it
readable at the tested widths. Existing runtime identity tests remain applicable
because the change projects already-owned metadata. **No skill change warranted.**

## Tiny Aya catalog and specialist assessment, 2026-10-04

Data modeling kept installable settings in the existing v1 catalog and public
recommendations in Results. Exact Global/Water execution identities permitted
reuse of their measurements; Fire/Earth request checks did not inherit those
quality claims. A quiet Swahili weekday error warranted a compact first-answer
record with a post-hoc interpretation, while routine successful checks needed
no new evidence registry. **No skill change warranted.**

## Closeout format

Each note records:

1. phase, date, and the product artifacts or decisions reviewed;
2. skills that materially influenced the work — omit incidental loads;
3. guidance that helped and where it changed the result;
4. missing, misleading, over-specific, or costly guidance, including a defect
   or near miss when one exists;
5. proposed improvement and owning skill/seam, or **no change warranted**;
6. disposition: proposed to the skills plan, accepted, declined, or awaiting
   more real-world evidence.

Keep product-specific facts here. Any proposal transferred to the skills plan
must state the context-independent invariant and preserve the skills
repository's routing, stance, and eval gates.

## Baseline — native manifest complete plus installed-base work through 2026-08-21

**Status:** retrospective native-manifest closeout and provisional
installed-base observation. This does not close the software-supply or Field
Kit execution-base work; each still receives its own note.

**Artifacts reviewed:** native manifest/lock/render/check contracts and tests;
software-supply catalog, software lock, adapter family, signed-catalog
lifecycle, tested-status read, installation contract, and pure install planner.

### What helped

- **Data modeling:** surface-first and one-home-per-fact kept user intent,
  resolution, tested evidence, observed installation, and removal authority in
  separate manifest, lock, catalog, receipt, and root-state artifacts. That
  separation prevented the lock from claiming an installation and the receipt
  from becoming update policy.
- **Code organization and unit design:** the keyed adapter family gave
  `system-package` a portable strategy while keeping Homebrew and uv vendor
  details at concrete edges. Resolver reads, pure selection, inspection, and
  installation effects remained separate instead of growing OS/package-manager
  conditionals in CLI verbs.
- **Reliable effects:** staged validation, one commit point, explicit unknown
  outcomes, and reconciliation shaped both catalog activation and software
  installation. They prevented a package-manager effect from being mistaken
  for an atomic local transaction.
- **Testing:** test-by-unit-kind produced pure policy/planner tables, focused
  adapter contract suites, and hermetic effect orchestration. The result tests
  product outcomes without invoking the network, Homebrew, uv, or the live
  service.

### What the work exposed

1. **Provenance needed a smaller trust grain.** A software lock may combine
   independently authorized catalog, experiment, and base identities. A
   top-level source list says who participated but cannot justify each
   independently trusted decision.
2. **Shared-resource removal needed stronger authority.** Per-installation
   receipts are history, not permission to remove a shared package. Safe
   uninstall needs root-wide acquisition provenance, prepared/active claims,
   exact identity, serialized final retirement, and preservation when proof is
   absent.
3. **Effect testing needed named crash boundaries.** Happy-path plus generic
   failure tests do not cover the durable worlds between prepared intent,
   provider effect, observed poststate, receipt publication, and claim
   finalization.
4. **Long-lived contract evolution is a distinct question.** Catalogs may
   change more often than the binary while locks and receipts must remain
   interpretable. Schema labels alone do not answer reader/writer coexistence,
   canonical identity, absent/null semantics, or removal of old forms.
5. **Unit-level lifecycle remains under-owned.** The reliable shared-resource
   protocol now has a home, but a unit or library surface still needs a clear
   owner-versus-borrower rule for starting, stopping, closing, cancellation,
   retries, logging, and global configuration.

### Proposals and disposition

- **Accepted into existing craft guidance:** provenance at the independently
  trusted grain (`data-modeling`); destructive/shared-resource authority
  (`reliable-effects`); failure-boundary restart tests (`testing`).
- **Planned:** deterministic craft-set verifier; `contract-evolution`; the
  `unit-design` half of resource/lifecycle ownership after another phase note
  confirms the seam.
- **No change warranted:** no Temper evidence currently supports changing
  routing descriptions, adding a craft meta-skill, or expanding functional or
  language coverage. Those remain governed by their existing set-level
  evidence gates.

No model eval was needed to reach these observations. They came from product
contracts, implementation seams, and concrete failure windows encountered in
the phase work.

## Software supply complete, 2026-08-24

**Status:** formal software-supply engineering closeout. Enabling and publishing the
already signed Pages source remains an explicit release operation, not an
unfinished product-code effect.

**Artifacts reviewed:** typed software-supply catalog and exact software lock;
shared selection and atomic resolution; authenticated catalog activation and
retained release signer; Homebrew, upstream-release, and uv resolver edges;
the isolated upstream-release installation member; tested-status derivation;
and the hermetic command/effect suites that exercise those boundaries.

### What helped

- **Code organization and unit design:** provider reads, pure translation and
  selection, and installation effects remained distinct units. The uv work
  added one process reader, one HTTPS reader, and pure Python-metadata/PEP 751
  translators without changing the shared resolver or lock writer.
- **Data modeling:** the interpreter is an exact adapter-native closure unit,
  not an ambient machine fact. Keeping policy edges in the catalog and exact
  artifacts in the lock made it possible to consume uv's flattened PEP 751
  install set without pretending that it supplied dependency metadata it does
  not contain.
- **Reliable effects:** signed publication stages and validates complete bytes
  before one commit, and the retained signer makes key input an explicit
  release boundary rather than a temporary code edit. The upstream-release
  installer likewise publishes one validated generation and reconciles from
  inspectable state.
- **Testing:** every external protocol has an injected hermetic edge with
  bounded output, cancellation, no hidden retry, protocol-drift refusals, and
  end-to-end validation through the shared lock invariants. Real scratch work
  remained an announced separate gate.

### What the work exposed

1. **Stable format does not imply sufficient semantics.** PEP 751 is a stable
   artifact format, but uv 0.12 intentionally emits a flattened install set.
   An adapter must state what information was absent and use an owned,
   conservative projection rather than inferring a richer graph.
2. **Upstream protocol versions are one compatibility unit.** uv's executable
   version, version-tagged managed-Python metadata, command flags, and emitted
   pylock shape must be reviewed together. Accepting a new version while
   validating only one of those surfaces would create a false compatibility
   claim.
3. **Release secrets need a permanent narrow interface.** Recreating signing
   code for each publication obscures review and increases key-handling risk.
   A retained stdin-only command makes validation, dry-run, and clean reruns
   part of the product's release machinery without storing private material.

### Proposals and disposition

- **Awaiting more evidence:** carry the first two observations into the
  already planned `contract-evolution` work as candidate examples of a
  version-coupled upstream protocol and an explicitly lossy adapter
  projection. Software-supply completion alone does not justify changing that
  guidance yet.
- **No additional craft change warranted:** the existing organization,
  modeling, effect, and testing guidance covered the signer and all three
  adapter shapes without a new routing rule or skill. Reassess lifecycle
  ownership and cross-repository contract testing at the Field Kit
  execution-base closeout.

No fine-tuning or model evaluation was relevant to this phase. The engineering
decisions followed from inspectable provider protocols, typed contracts, and
failure-boundary tests.

## Embedded Field Kit runtime closeout (2026-08-27)

### Applied guidance

- **Code organization and unit design:** the ownership decision became one
  vertical `fieldkit` slice inside Temper: pure catalog/session/planning units,
  a command orchestrator, and narrow protocol/process/resource effects. The
  adjacent Field Kit repository now contains only promotion content.
- **Data modeling:** package revision 2 names an exact Temper protocol identity
  instead of a script path. The consented session binds catalog, package,
  material, machine, executing binary, software lock, outcome, stage evidence,
  protocol report, and final report without a moving runtime dependency.
- **Reliable effects:** start materializes immutable embedded bytes into a new
  owned root before one atomic external session commit. Each stage validates
  all consented inputs, records failed output without advancing, and commits a
  successful transition once. Restore remains marker- and confirmation-gated.
- **Testing:** hermetic workflow tests cover keep/restore, consent refusal,
  resumability, protocol dispatch, and embedded package validation. Protocol
  unit tests cover exact identity, structured-only evidence, resource stops,
  and refusal before effects. Live model/download work remains separately
  authorized.

### What the work exposed

1. A content repository and its runtime do not need the same distribution
   boundary. Embedding reviewed content in the effect-owning binary removes a
   second build and a second executable identity without collapsing Labs,
   promotion, and product-review authority.
2. Runtime behavior needs a versioned identity of its own. Replacing a bound
   script with `{id, revision, schema}` lets the package remain declarative and
   makes unsupported behavior fail at catalog verification.
3. Consent is stronger when it binds materialized bytes. Copying the exact
   embedded package and machine facts into the dedicated root makes every
   resumed stage check the same local evidence rather than trust a checkout.

### Proposals and disposition

- Add embedded/source byte-parity as a permanent release check when the release
  workflow is formalized.
- No craft-skill change is warranted yet; the existing vertical-slice,
  explicit-effect, canonical-modeling, and hermetic-test guidance directly
  covered the ownership refactor.

No fine-tuning or model evaluation was relevant. The problem was distribution,
authority, and effect ownership rather than learned model behavior.

## Additive V3 execution-lock slice (2026-09-13)

Data modeling kept portable compatibility, exact release material and observed
machine facts separate. The real Qwen install/bind/probe witness exposed a
remaining exact-host comparison in the binding reader; a contract test now
covers compatible locks across observed macOS versions while preserving legacy
exact-host refusal. Code organization and unit design kept compilation pure in
`internal/catalog`, publication in the command layer, and runtime effects in
their existing owners. Reliable effects guided non-replacing file publication
and exact partial-export repair; testing covered those outcomes before the
isolated runtime witness. Field Kit consumes a public export contract rather
than copying the graph compiler. No craft-skill change is warranted from this
single slice; the compatibility reader incident is the concrete seam to watch.

## Direct runtime release closeout (2026-09-22)

The first native alpha.8 check exposed a false lifecycle assumption in the
hermetic helper: real llama-swap starts an engine group of its own and uses a
basename for argv[0]. The helper now reproduces both behaviors. Temper reads the
kernel executable path, binds each group's identities, and proves every group
absent before cleanup. Field Kit measures those identities without owning their
shutdown. The corrected alpha.9 candidate passed the bounded native check; the
failed attempt and refusal were retained during diagnosis. Existing reliable
effects and testing guidance covered the correction; no craft change is needed.

## Catalog distribution (2026-09-22)

The current catalog reuses the existing signing trust and bounded HTTP reader.
Its signed channel owns publication sequence; authored model records do not
gain release bookkeeping. Explicit rollback retains the highest accepted
publication, so local recovery does not weaken network downgrade refusal.
One atomic state commit and a kernel writer lock cover update/rollback races;
the tests exercise interrupted staging, offline use and preserved user files.
Latest compilation retains the exact authenticated source digest while resolving
new software inputs. These are applications of the existing organization,
data-modeling, reliable-effects and testing guidance; no skill change is needed.

## Guided setup (2026-09-23)

The wizard reuses Selection and Execution Lock instead of adding a second
configuration schema. Pure planning, read-only catalog/model inspection, the
terminal state machine and filesystem publication have separate owners. An
accepted review binds exact bytes so a moving latest release cannot change
between review and preparation. Exclusive directory publication keeps multiple
mode selections and locks complete; resume preserves edits and credits existing
model sets when checking remaining disk space.

The native terminal check caught a review viewport that snapped back after
scrolling; a wrapped, small-terminal regression now covers it. Runtime review
also exposed the distinction between preparation, temporary supervised serving
and persistent helper availability. The wizard states the existing single-layout
and idle/restart limits; managed activation remains separate. Hermetic machine
fixtures establish selection behavior, not low-memory model fit. These findings
fit the existing craft guidance; no skill change is proposed.

Review follow-up found that equal model records did not prove installed-byte
reuse: template variants still fetched duplicate weights. Fetch regressions now
assert one model transfer, shared file identity, byte verification and rollback
after template failure. Model reuse stays within existing artifact receipts,
without a cache registry. The same review separated harness ownership from a
valid layout ID and extended viewport tests to wrapped selection rows. These
are corrections at the owning effect and rendering boundaries, not reasons to
add another configuration or lifecycle abstraction.

Trying Latest exposed a source-contract gap: the fake GitHub response represented
only numbered stable tags, while llama.cpp publishes its binary builds as
prereleases alongside semantic stable-release headings. The adapter now owns
that concrete release convention. Hermetic tests replay those release shapes
through the actual wizard resolver, including failed integrity and no-write
outcomes; a read-only upstream preview checked the fixtures against current
publication behavior. Existing testing guidance already calls for faithful
external contracts, so no skill change is warranted.

The presentation follow-up separated the persistent tabs and controls from
scrollable content, while keeping the same explicit-selection state machine.
One set of disclosure sections now feeds both plain CLI lines and styled review
blocks. Rendering checks cover actual terminal bounds and readable focus rather
than freezing an ANSI snapshot. Visual inspection removed a duplicated summary;
resize tests ensure hidden confirmation controls cannot be activated. Existing
unit-design and testing guidance covered the work; no skill change is warranted.

MacBook use exposed gaps that frame bounds alone did not establish: advancing
needed a visible control, scrolling needed ordinary keys, and duplicate
download totals hid whether weights were already present. The follow-up keeps
cache facts in the planner, projects one per-file status into CLI and TUI, and
retains the transfer summary when rows collapse. Regressions exercise visible
mouse positions and explicit-selection guards, skipped fixed templates, arrow
and wheel scrolling, and cache/no-cache disclosure. These are concrete usability
and integration checks under the existing guidance. Native inspection also
caught nested table wrapping; width regressions now preserve complete filenames,
statuses and table borders. No new craft rule is needed.

Shared-cache work initially grew a second implementation of HF's publication,
locking and partial-download lifecycle. The owner review corrected that boundary:
the official hf client now owns downloads and cache writes, while Temper owns
read-only preview, exact-byte verification and durable installation. Tests cover
that integration contract and the effects Temper still owns, including child
cancellation, corrupt bytes and cache/install removal independence. Downloader
support uses the existing hf or uv tool path without another dependency registry.
This applies the existing narrow-boundary and reliable-effects guidance; no new
skill mechanism is warranted.

Context review exposed a historical test condition being reused as a product
default. Maintained catalog layouts now own the native maximum; Selection owns
the accepted number and the existing lock/renderer carries it exactly. No new
capability registry or model-memory estimator was introduced. Frozen evidence
and old locks retain their original windows. Regressions check actual rendered
context arguments, independent mode choices, invalid input, exact resume and
unchanged weight identity. This applies the existing fact-ownership and
assertion-strength guidance; no skill change is needed.

## Catalog grouping and installed alternatives — 2026-09-26

The [setup contract](contracts/init.md) separates the models selected for
installation from the default local foreground. Data-modeling guidance kept
catalog labels, estimated memory tiers and editorial order outside execution
identity. The existing `local` selection/lock pair owns the default; alternatives
have their own exact pairs, without a second mutable default registry.

Reliable-effects guidance kept publication at the existing atomic configuration
boundary. Testing guidance directed regressions at independent template/context
choices, missing or ambiguous defaults, interrupted/refused saves, clean reruns
and offline preparation from every saved lock. A native terminal dry run checked
the grouped choices and per-model traversal. Existing guidance covered the
change; no skill change is warranted.


## Splash release integration — 2026-09-26

The [execution contract](contracts/execution-runtime.md) keeps draft identity,
source weights, derived tokenizer and native conversion cache at their own grains.
Splash owns architecture matching and conversion; Temper invokes the selected
release's metadata routine and owns exact inputs, publication and supervision.
This avoided copying upstream model-loading algorithms or introducing another
installer. The prebuilt archive exposed valid tar record padding that the shared
archive reader previously refused; bounded zero padding is now admitted with
checksum, payload and size-limit regressions.

Failure tests cover draft hash rejection, retained shared-cache ownership,
retry after cache repair, target reuse, concurrent preparation, changed metadata,
and exact frontend/native command ancestry. Harmless native child processes test
three-role ownership and shutdown. Existing craft guidance was sufficient.

The authorized native check then caught a source-format boundary omitted by
metadata-only tests: Splash requires a `model.json` descriptor to choose its
source loader. A focused assertion now checks that contract; startup diagnostics
are exposed through the router. After correction, the real 1.1.0 release served
short chat and tool requests and proved owned shutdown. The integration smoke
was kept separate from context-capacity and quality evidence.

## Writing companions and external GGUF drafts — 2026-09-27

The existing draft record now carries either an exact GGUF assistant or Splash's
safetensors pair. Data-modeling guidance kept its revision and file identities
independent of the target and serving software: changing a draft invalidates the
composition without changing those components. Rendering owns the native MTP or
DFlash flags, explicit draft GPU placement and F16 draft KV; callers do not
assemble alternate shell commands. Regressions check the selected file through
compilation and rendering, invalid material and independent invalidation.

The authorized Rapid comparison exposed a real child-process boundary missing
from software-only preparation: a separate crash-log helper. Reliable-effects
guidance kept recovery separate from a refused ownership observation. The
reviewed helper is admitted only by its script digest, selected interpreter,
engine parent and normal PID/start/argv/group identity. Tests cover changed
commands, wrong parents, reparenting after exit, duplicates and restarts. A native
request then completed with proved helper shutdown. Another transient llama.cpp
shutdown refusal remains unexplained; diagnostics now identify its process row,
without relaxing the guard or claiming the underlying cause was fixed.

Testing guidance kept forced-length acceleration, filled-input retrieval and
completed writing/tool work as separate evidence. Model mistakes survived some
speed/precision tuning, so the catalog descriptions retain concrete role limits.
The exact selected native configurations must survive editorial catalog naming
unchanged. Existing craft guidance was sufficient; no new skill rule or schema
registry was needed.


## Presets and user-owned layouts — 2026-09-28

Data-modeling guidance separated preset execution facts, catalog editorial copy,
user composition and observed activation. V3 authoring names the new concepts;
issued locks keep their historical meaning. Exact software closures are reusable
independently of a layout name. The [configuration contract](contracts/layouts.md)
is the single owner of these boundaries.

Reliable-effects guidance exposed an orphan risk in router-only recovery:
llama-swap starts engines in separate process groups. The managed path now
records each engine lifetime before exec and verifies kernel identity and exact
argv before recovery signals. The launcher replaces itself; it is no additional
daemon. Interrupted desired-state commits and provider responses are tested at
their durable boundaries, separately from fixed-process Field Kit supervision.

Testing guidance kept catalog selection, mixed rendering, memory predictions,
disposable-process ownership tests and native serving as different claims.
The [authorized bounded native smoke](design/managed-layout-smoke.md#observed-result)
exposed a mismatched fake: v260 omits an empty in-flight request list. The
regression now uses the producer's actual wire form. Idle unload also exposed
an `ESRCH` race between two reads; bounded complete-snapshot retries preserve
ownership refusals while handling that normal lifecycle transition. Cold-cache
timeout and startup admission remain limits of the observation. Existing craft
guidance was sufficient; no skill edits or new evidence registry were needed.

## Alpha schema cleanup and release — 2026-09-29

Data-modeling guidance kept historical context identities and issued snapshots
unchanged while removing obsolete runtime readers. Testing used the public
compiler and configuration commands across Field Kit's frozen matrix. A real-host
preview exposed an output-parent precondition missing from the fake; the preview
now uses the existing private directory without writing a lock. Reliable-effects
guidance kept the bootstrap pin tied to the downloaded signed archive and checked
fresh setup plus unchanged replay. The local maintainer plan records delivery
verification. Existing guidance was sufficient; no skill change is proposed.

## Optional output defaults — 2026-10-16

The data-modeling and unit-design guidance kept a required integer from standing
in for a product need: an omitted cap now has one semantic meaning, with native
translation owned by each adapter. The
[output-default contract](contracts/execution-lock.md#settings-and-evidence)
records Rapid's actual unsupported case instead of applying that restriction
to every engine. Tests reproduced the rejected omission and coupled context
bound, then covered lock/configuration round trips, rendered defaults and an
explicit update from capped to uncapped settings. Full offline checks do not
claim long-output model behavior. Existing guidance was sufficient; no skill
change or additional abstraction was needed.

## Homebrew distribution — 2026-09-30

Ruby guidance followed Homebrew's formula conventions. Reliable-effects guidance
kept immutable bottle assets ahead of the formula update and retained published
bytes on retry. Testing exercised a real source install and bottle pour in an
isolated prefix. Homebrew's local-package-path policy required the normal trusted
formula and checksum-cache route; a syntax check alone would not have exposed
that installation boundary. The formula's functional test reads an empty user
configuration without creating a root or requiring a GPU. Existing guidance was
sufficient; no skill change is proposed. Bottle asset publication remains pending.
