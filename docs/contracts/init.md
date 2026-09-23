# Guided setup: `temper init`

The wizard uses the existing modes-first design. A compact chat model can be
the local main model; utility mode instead leaves the main model with the
user's harness. Eligibility depends on the selected configuration and observed
machine facts, without a fixed RAM threshold or model-size role assignment.

```text
temper init [--root PATH] [--catalog FILE] [--dry-run]
temper init [--root PATH] [--catalog FILE] --profile ID [--profile ID]
  [--template LAYOUT=PATCH|builtin] [--software recorded|latest|tested]
  [--context LAYOUT=TOKENS]
  [--prepare] [--dry-run] [--json]
temper init [--root PATH] --resume [--prepare] [--dry-run] [--json]
```

The default root is `~/.temper`; an explicit root overrides it. With no
`--profile` arguments the terminal wizard asks for modes, a profile per chosen
mode, available template alternatives, context windows and software policy, then
presents a combined review. Profile
arguments explicitly select a configuration in scripts. Catalog profiles can
compose several layouts, but the current foreground runtime supports one.
The first wizard therefore offers one single-layout profile per mode; an
explicit scripted composite can be saved but cannot be prepared through init.

The terminal presentation uses Lip Gloss with Tokyo Night colors. A persistent
tab bar identifies each screen. A mode gets a Templates tab only when its chosen
profile has more than one available template for a model; fixed defaults are
still saved. Choices and explanatory details use separate blocks.

Context defaults to `auto`: the largest reviewed test point matching the
detected machine and selected configuration. The Context tab shows any known
default for recorded software and the current template; review resolves the
chosen software before making the final match. If none applies, preview reports
unknown and requires an explicit token count. `a` restores automatic selection.
The model's ceiling bounds manual values; it does not establish memory fit.
The window includes input and output and must exceed the output allowance.
There is no interpolation across hardware, contexts or runtime settings, and
latency advice does not reduce the capacity default. Evidence records are
defined in the [catalog contract](execution-lock.md#owned-records-and-scope).

Model choices show the artifact's brief description and optional assessment
link. File-size and memory predictions are separate. Workshop can edit the
description using the owner's assessment; updates preserve that wording.

Each choice screen has a Next button, selectable with Up/Down and Enter or a
mouse click. Tab, Right or `n` also advances after explicit selection;
Shift+Tab/Left or Esc returns to the preceding screen. Review uses Up/Down and
the trackpad/mouse wheel to scroll, Left/Right or Tab/Shift+Tab to select an
action, and Enter or a button click to confirm. Esc returns to Software.
PageUp/PageDown remain additional scrolling shortcuts. Actions and help stay
outside the scrollable content. Narrow terminals keep the active tab visible
and wrap content without losing focused choices.

On Context, Left/Right, Backspace and Delete edit the focused number; Ctrl+U
clears before the cursor. Up/Down selects a field or Next, Enter accepts, and
Tab/`n` advances after validation. Esc/Shift+Tab goes back without losing the
entered value. Model and mode choices keep independent context settings.

The default source is an already verified local catalog. On a fresh root,
setup reads and verifies the signed stable publication in memory. It does not
persist that catalog preview; the saved locks retain its exact source digest.
Use `catalog update` to maintain a local cache. An explicitly
supplied local catalog is an authoring input, displayed as such. No network
fallback replaces an invalid local catalog. Recorded software resolution is
offline; latest/tested may read upstream metadata and archives and must satisfy
the same requirements as `catalog compile`. Latest uses downloadable llama.cpp
nightly builds and stable llama-swap releases; the software screen discloses
that choice.

The review discloses the exact choices, model/template/software downloads,
fresh and remaining disk allowances, free disk and labeled memory predictions.
Model weights are a lower bound on runtime memory; unknown KV-cache and engine
overhead remain explicit. Existing model files with matching receipt hashes and
file shapes reduce the remaining allowance, including files in other template
or layout compositions; malformed matching material is refused. Software
space stays a conservative fresh-install allowance. This read-only planning
inspection does not replace preparation's byte verification.
The Downloads block leads the review with a weight-transfer summary that stays
visible when the file table is collapsed. Press `d` or click its heading to
expand or collapse it. Each file lists its size and preparation status:
**Cached in Temper**, **Cached in Hugging Face**, **Download on Prepare**, or
**May download** for software whose installation is checked during preparation.
Inspection covers `ROOT/artifacts/layouts`, including other layout/template
compositions, and the standard shared HF cache. HF environment overrides are
honored; the exact cache location is displayed. The
[fetch contract](fetch.md#shared-hugging-face-cache) owns cache lookup and the
official downloader invocation. Save downloads no
weights; Prepare fetches missing files and verifies cached weights.
Modes are alternative configurations, never a promise that
all selected modes will run simultaneously.

The wired-memory check conservatively budgets all model weights when GPU
offload is enabled; partial-offload savings are not estimated. The preview
counts each distinct model hash once across selected modes. Preparation reuses
model files through hard links while keeping each layout/template composition's
identity and receipt separate. A cache on another filesystem requires a verified
copy: preview credits the saved network transfer but includes the installation
copy in its disk allowance. New cache bytes on a separate filesystem have their
own free-space check; shared-filesystem model storage is counted once. Preparing
with neither hf nor uv available gives an actionable support-tool error. When uv
must provision hf or Python, those support-tool downloads and cache overhead are
explicitly unestimated. Template downloads remain specific to each set.

Templates are proposed per-model defaults that the user can override with a
compatible patch or the model's embedded template. Save records the accepted
template explicitly. Each resulting lock fixes its resolved revision and bytes.
Scripted `--template` applies to every selected profile containing that layout;
the interactive per-mode screens can choose different templates for each mode.
Tested software is disabled when a selected profile lacks the required evidence.

Context choices are saved in Selection's `context_windows`, and the exact lock
and engine command use that chosen number. For example:

```sh
./build/temper init --catalog catalog/guided-setup.json \
  --profile qwen3.8-27b-q4xl-local \
  --context qwen3.8-27b-q4xl-mtp=65536 --dry-run
```

Omit `--context` to request the largest applicable tested context. The current
authoring candidate contains no such findings yet, so an explicit window is
required and its fit is reported as unknown. Repeated
`--context LAYOUT=TOKENS` flags apply to all selected profiles containing those
layouts; interactive mode can choose different windows per mode. Old saved
choices and `--resume` retain their exact windows. The published stable catalog
still has its historical 32k default until an explicit catalog release; there
is no client-side rewrite of a signed catalog or saved lock.

## Saving and preparation

One atomic directory publication creates `ROOT/configuration` containing
`local.selection.json` and `local.execution.lock.json`, and/or the equivalent
`utility` pair. These are the V3 user selections and exact execution inputs;
the existing explicit manifest workflow remains available. Existing user
manifests and Pi configurations are preserved.

Cancel in the wizard or a failed preview creates no root, configuration,
installation or model download. After an explicit Save, an interrupted write
may leave the empty root and its coordination lock, but never a partial
configuration. Dry run reads and reports only. An identical
save is unchanged; differing existing configuration is reported and never
overwritten. The user can choose a separate root to review a different setup.
Concurrent saves serialize through a kernel-released lock and compare the
complete configuration before committing.

Save installs nothing and fetches no model files. Resolving latest/tested may
read release archives during preview to verify exact software inputs.
Prepare commits the reviewed configuration, then invokes `execution prepare`
for each selected mode using
an immutable private copy of the reviewed lock bytes. Installation failures
retain the configuration and existing
installation receipts, plus completed files and resumable state in the shared
HF cache. Temper never prunes that cache. `--resume --prepare` reads those saved locks and retries
without re-resolving moving upstream defaults. It verifies the saved Selection
still matches the lock, and rechecks machine and disk facts before effects.

Preparation verifies material and renders configuration. It starts no model
process and installs no service. Completion shows a command for a temporary
supervised foreground session. Once its engine has been observed, idle unload
or restart ends that session. Persistent helper availability needs the later
managed lifecycle; existing Field Kit supervision rules remain intact.
Pi remains user-managed in its own home; the wizard does not
activate a Pi integration. Available integrations and tools need their own
implemented catalog and rendering path before the wizard can offer them.
Each foreground start requires a new status-file path; completed supervision
records are retained.

## Verification

Hermetic checks cover compact models as the main model, a large model excluded
on a small machine, helper-only utility profiles, incompatible templates,
insufficient disk, cancellation, dry-run purity, complete atomic configuration
publication, concurrent saves, preserved user edits and resume from exact locks.
UI tests exercise explicit selections, skipped template screens, keyboard and
mouse navigation, automatic and explicit context values, invalid context
refusals, arrow/wheel scrolling, collapsed disclosures, responsive file
tables, preview failures and cancel. Cache fixtures verify absent, reusable and
malformed material without model downloads. Shared-cache regressions cover
environment precedence, snapshot/blob layouts, cache status, distinct disk and
network allowances, corrupted bytes, official-client invocation, failure and
cancellation, and installation/cache removal independence.
These checks establish setup behavior, not native model quality or low-RAM fit.
