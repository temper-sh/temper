# Guided setup: `temper init`

The wizard uses the existing modes-first design. A compact chat model can be
the local main model; utility mode instead leaves the main model with the
user's harness. Eligibility depends on the selected configuration and observed
machine facts, without a fixed RAM threshold or model-size role assignment.

```text
temper init [--root PATH] [--catalog FILE] [--dry-run]
temper init [--root PATH] [--catalog FILE] --profile ID [--profile ID]
  [--default-profile ID]
  [--template LAYOUT=PATCH|builtin] [--software recorded|latest|tested]
  [--context LAYOUT=TOKENS]
  [--prepare] [--dry-run] [--json]
temper init [--root PATH] --resume [--prepare] [--dry-run] [--json]
```

The default root is `~/.temper`; an explicit root overrides it. With no
`--profile` arguments the terminal wizard asks for modes, models to install,
the default local model, available template alternatives, context windows and
software policy, then presents a combined review. Each installed local choice
has its own single-layout profile and exact lock. Utility mode still selects
one helper profile, leaving the main model with the harness. A catalog profile
can compose several layouts, but the current foreground runtime supports one;
an explicit scripted composite can be saved but cannot be prepared through init.

On the local model screen, Space or the checkbox toggles installation. Enter,
`d`, or clicking the rest of a card chooses that model as the default and also
selects it for installation. Changing the default retains previously selected
models. Removing the default requires a new explicit default choice before
continuing. No choice starts selected. Templates and context are configured for
each installed choice, beginning with the default; switching or going back
preserves each choice's settings.

In scripts, repeat `--profile` for local alternatives and use `--default-profile`
to name one of them. A sole explicitly selected local profile is necessarily the
default, so the flag is optional in that case. Multiple local profiles require
the flag. An unselected or utility profile cannot be the local default.

The terminal presentation uses Lip Gloss with Tokyo Night colors. A persistent
tab bar identifies each screen. A selected configuration gets a Templates tab
only when it has more than one available template for a model; fixed defaults are
still saved. Choices and explanatory details use separate blocks. When several
local models are selected, their Templates/Context tabs show the model number.

Context defaults to `auto` when the layout has reviewed context findings: the
largest test point matching the detected machine and selected configuration.
Without any findings, the Context tab starts with an empty token field and
requires an explicit number before continuing. It does not offer an automatic
choice that is known to be unavailable.

The tab shows any known default for recorded software and the current template;
review resolves the chosen software before making the final match. If none
applies, the wizard returns directly to that model's Context field and asks for
a number. It preserves the other mode, template and software choices. Missing
context evidence is an input request, not a retryable preview error. `a`
restores automatic selection where findings exist; it does not fill an unknown
window from the model ceiling or authored example.
The model's ceiling bounds manual values; it does not establish memory fit.
The window includes input and output and must exceed the output allowance.
There is no interpolation across hardware, contexts or runtime settings, and
latency advice does not reduce the capacity default. Evidence records are
defined in the [catalog contract](execution-lock.md#owned-records-and-scope).

Model choices show the artifact's brief description and optional assessment
link. File-size and memory predictions are separate. Workshop can edit the
description using the owner's assessment; updates preserve that wording.

The model chooser groups configurations by the catalog's memory tier, largest
first (XXL through XS), with the letter and capacity range together. Available
groups precede unavailable configurations. Tier placement is navigation, not a
fit guarantee; the machine checks still determine availability. Within each
group, the catalog's `layout_order` supplies the editorial order. Reordering a
catalog never selects a model or changes a saved default. Each choice identifies
its Model, Weights and Engine separately. Current tier placements are labelled
estimates; they do not establish fit across the whole capacity range.

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
Memory recommendations lead the review when applicable. The Downloads block
then shows a weight-transfer summary that stays visible when the file table is
collapsed. Press `d` or click its heading to
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

When the predicted GPU budget leaves less than 2 GiB or 10% of the current
wired limit, setup highlights **Memory budget is tight** in the model choice
and at the top of review. The same recommendation appears in scripted output;
JSON includes `wired_memory_advice` per mode. Blocked preparation prints the
remedy too. CPU-only setups receive no GPU-limit recommendation, and comfortable
budgets receive no warning. Utility helpers use their GPU weight allowance even
when the local foreground wall model does not apply.

Current machine budgets come from Metal's `recommendedMaxWorkingSetSize`,
labelled `live-metal`, rather than a percentage of RAM or the configured sysctl
override. An unavailable Metal reading is an error. Historical saved machine
facts retain their original values and labels.

The recommendation reuses the existing budget prediction. Its margin is setup
policy, not a new estimate of context or engine memory. The proposed limit
uses whole GiB with at least 2 GiB and 10% of the proposed limit spare,
stays at or below 85% of physical RAM, and leaves at least 4 GiB for macOS and
other applications. That reserve is **Temper policy, not a detected macOS
maximum**. The recommendation also accounts for the holder's fraction envelope
growing with a higher Metal budget; it does not recommend an increase that
would immediately leave the recalculated prediction too tight.
If those bounds prevent an adequate increase, setup explains that a smaller
model or context is needed instead of printing an excessive limit. Alternative
modes produce one command for the largest budget, not their sum. A warning
does not change admission or establish that an unmeasured context fits.

The highlighted block supplies a machine-specific `[manual]` command. In another
Terminal, record the current value with `sysctl -n iogpu.wired_limit_mb`, apply
the displayed `sudo sysctl iogpu.wired_limit_mb=NUMBER` command if the setting is
available. The administrator password is entered in Terminal; Temper never runs
this command. Verify with `temper machine facts`: `wired_limit_mib` must report
the intended effective budget with `wired_limit_source: live-metal`. A changed
sysctl value alone does not establish that Metal reports the new budget. If it
still reports the old budget, do not keep raising the override.

Quit and rerun the same setup command to refresh all choices. Every review
preview/retry rereads machine and disk facts, and preparation rechecks them
before saving or installing. Saved historical predictions are explicitly
labelled and never passed off as live Metal readings.

This change lasts until reboot. To undo sooner, stop the model and restore the
recorded value using the same `sudo sysctl` command. Raising the allowance adds
no physical RAM; other applications may need more than the reserved space.
These instructions follow the documented
[MLX system wired-limit command](https://github.com/ml-explore/mlx-lm#large-models).
Apple describes its [recommended working set](https://developer.apple.com/documentation/metal/mtldevice/recommendedmaxworkingsetsize)
as a performance budget; Temper does not claim a separate maximum accepted
override.

Templates are proposed per-model defaults that the user can override with a
compatible patch or the model's embedded template. Save records the accepted
template explicitly. Each resulting lock fixes its resolved revision and bytes.
Scripted `--template` applies to every selected profile containing that layout;
the interactive screens can choose different templates for each configuration.
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
layouts; interactive mode can choose different windows per configuration. Old saved
choices and `--resume` retain their exact windows. The published stable catalog
still has its historical 32k default until an explicit catalog release; there
is no client-side rewrite of a signed catalog or saved lock.

## Saving and preparation

One atomic directory publication creates `ROOT/configuration`. The chosen
default uses `local.selection.json` and `local.execution.lock.json`. Additional
local choices use `local.PROFILE.selection.json` and
`local.PROFILE.execution.lock.json`; the optional helper uses the `utility`
pair. The `local` pair is the authoritative default; a separate default registry
is unnecessary. Resume validates complete pairs, their identities and a present
default before any installation. These are the V3 user selections and exact
execution inputs; the existing explicit manifest workflow remains available.
Existing user manifests and Pi configurations are preserved.

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
for each selected configuration using
an immutable private copy of the reviewed lock bytes. Installation failures
retain the configuration and existing
installation receipts, plus completed files and resumable state in the shared
HF cache. Temper never prunes that cache. `--resume --prepare` reads those saved locks and retries
without re-resolving moving upstream defaults. It verifies the saved Selection
still matches the lock, and rechecks machine and disk facts before effects.

Preparation verifies material and renders configuration. It starts no model
process and installs no service. Completion shows a command for a temporary
supervised foreground session for each installed choice, with the default local
model first and labelled. The default retains installation ID `setup-local`;
alternatives use `setup-local.PROFILE`. These are independently started
configurations; installing several does not promise simultaneous residency.
Once its engine has been observed, idle unload
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
Multi-model regressions cover independent install/default choices, complete
saved pairs, ambiguous or invalid defaults, default-first preparation and exact
offline resume of all alternatives. Catalog grouping respects availability,
descending tiers and editorial order; presentation edits preserve execution
identity.
UI tests exercise explicit selections, skipped template screens, keyboard and
mouse navigation, automatic and explicit context values, invalid context
refusals, arrow/wheel scrolling, collapsed disclosures, responsive file
tables, preview failures and cancel. Cache fixtures verify absent, reusable and
malformed material without model downloads. Shared-cache regressions cover
environment precedence, snapshot/blob layouts, cache status, distinct disk and
network allowances, corrupted bytes, official-client invocation, failure and
cancellation, and installation/cache removal independence.
These checks establish setup behavior, not native model quality or low-RAM fit.
