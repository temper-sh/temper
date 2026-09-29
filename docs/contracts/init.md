# Set up and manage presets and layouts

`temper init` and `temper configure` open the same editors. The default root is
`~/.temper`; `--root PATH` selects an isolated root. Temper uses a verified local
catalog or reads the signed stable publication in memory. Invalid local inputs
never trigger a network fallback.
Local authoring may pass `--catalog catalog/guided-setup.json` explicitly.
Use a fresh root when upgrading from alpha.10 or earlier.

```sh
temper init --dry-run
temper configure --root /private/temper-root
```

## Choose presets

Recommended and All share one selected set. All includes Recommended and the
smaller catalog choices. Availability, descending memory tiers and editorial
order organize the list; none selects a preset or default for you. Each preset
card has one **Model · Weights · Engine** header, followed by its authored
description, context and machine fit.

Thinking is controlled in the terminal agent. Current catalog presets default
to medium; a different default requires supporting test evidence. Thinking
levels are omitted from wizard cards. Changing the default creates new exact
execution settings; saved presets and historical measurements retain theirs.
Pi's `/thinking` menu and Shift+Tab select the level when its local provider is
configured for the engine's request controls. Managed setup does not yet install
Pi configuration; the explicit manifest renderer supplies the opt-in Pi export.

Keyboard navigation follows the same pattern throughout the editors:

| Key | Action |
|---|---|
| Up/Down | Move between rows, including filters, presets, fields and footer buttons. |
| Left/Right | Move between controls in the current row; inside a text field, move the caret. |
| Enter/Space | Activate the focused button or checkbox. Space types a space inside a text field. |
| Tab | Focus the forward button: Next, Review or Done; Apply in a form; Save in Review. |
| Shift+Tab | Focus Back, or Cancel on the first screen and in forms. |
| Esc | Return to the previous screen or cancel the current form. |

Tab and arrow navigation do not activate buttons. Enter or Space confirms the
focused action. From the first preset, Up reaches Recommended/All; Left/Right
changes the filter and Down returns to presets. Selections survive filtering
and navigation. Buttons remain reachable with arrows in narrow terminals;
chevrons indicate additional buttons outside the visible range.

Mouse clicks and letter shortcuts remain available: `a` changes the filter,
`c` edits the current preset's context, and `t` cycles its compatible templates.
Save and Prepare are available in Review; `q` or Cancel leaves without saving.
Recommended descriptions are required preset copy, independent of the weight
artifact and of benchmark rankings.

The editors retain the Tokyo Night presentation, bordered choices and fixed
tabs, actions and help. Scroll with the trackpad or PgUp/PgDown to read long
cards. Narrow terminals wrap the content while keeping navigation visible.

Selecting a preset accepts its exact authored settings, including context.
The context ceiling bounds edits; it does not establish memory fit. Exact
matching context evidence can mark a tested point. Otherwise review says fit
is unknown. Customized settings are named as customized and do not inherit
performance evidence for different inputs. Saved exact presets remain editable
when the moving catalog changes or removes a choice.

## Compose layouts

Local and Utility begin empty and can be renamed or removed. The footer offers
New, Rename and Remove; `n`, `r` and `x` are optional shortcuts. Enter on a
layout opens its presets. Up/Down moves between preset rows and the idle-unload
control. Left/Right focuses Included, Load on activation or Default within a
preset row; Enter or Space toggles that checkbox. Shortcuts remain available:

| Key | Choice |
|---|---|
| `s` | Load it on activation |
| `d` | Set or clear the optional default route |
| `i` | Set the idle-unload timeout in seconds |

Each checkbox also accepts mouse clicks. Tab focuses Done; Enter returns to the
layout list. In name, context and idle editors, Up/Down moves between the field
and Apply/Cancel, while Left/Right chooses a button or moves the text caret.
Invalid input remains available for correction; Enter in the field or the
Apply button accepts a valid value.

Default and startup require membership, independently. A helper-only layout can
have no default. Removing membership leaves any invalid default/startup choice
visible until explicitly corrected. Removing a referenced preset lists affected
layouts and refuses until those memberships are resolved. Deleting a layout
preserves shared presets and installed files.

Startup is initial loading, not permanent residency. The default idle timeout
is 1800 seconds. Review distinguishes the startup set from available alternatives.
When all estimates fit together, requests may use several included models. When
they do not, llama-swap serializes competing groups and evicts idle alternatives;
the startup set forms one group. Estimates account for weights and declared
engine caps. Unmeasured cache/runtime overhead remains unknown.

## Review, save and prepare

From Layouts, Tab focuses Review and Enter opens it. Left/Right moves among
Save configuration, Prepare installation, Back, Cancel and Retry preview.
Up/Down moves between the action buttons and Downloads. Enter confirms the
focused action or expands the file/size/cache table. Tab focuses Save;
Shift+Tab focuses Back. Scroll the review with PgUp/PgDown or the trackpad.
The buttons and Downloads heading also accept clicks; `d` toggles Downloads
and `r` retries the preview. Memory advice and preparation refusals remain
separate and visible. Each layout has a compact table of models, weights,
engines, context/output limits, loading choices and defaults, followed by its
idle timeout and memory estimates. Narrow terminals combine columns; selected
presets outside layouts appear in their own table. Review counts shared weights
and exact software closures once across selected presets and layouts, and lists
the affected layouts for shared preset edits. Different engine versions keep
separate installations.

Save atomically publishes `ROOT/configuration.json`. Prepare first saves, then
installs exact dependencies and fetches missing material. It starts no engine or
service. Matching receipts and provider checks credit installed software;
matching weight identities and cache paths reduce transfer and disk allowances.
Preparation still verifies bytes. HF cache copies on another filesystem count
as additional disk. Support-tool provisioning and runtime conversion overhead
remain visible estimates rather than measured costs.

Dry run and Cancel write nothing. Existing choices use a revision check under
an OS-released root lock; a concurrent edit cannot silently win. Incomplete
staging files are not current state. After failed preparation, resume the saved
locks offline:

```sh
temper configure --root /private/temper-root --resume --prepare
```

Machine and disk facts are reread before preparation. `--software recorded` is
the default; explicit `latest` or `tested` resolution may read upstream release
metadata and archives. These choices apply when compiling a new preset; resume
never resolves moving versions. Memory guidance never changes system settings.

## Script the same decisions

Select exact catalog presets, optionally customize settings or save one under a
new name. Omitting layout editing creates no implicit layout or default:

```sh
temper configure --root /private/temper-root --catalog catalog/guided-setup.json \
  --preset qwen3.5-4b-q4km-off --context qwen3.5-4b-q4km-off=16384 --dry-run --json
```

Omit `--dry-run` to save. Repeat `--preset` for several choices. `--template
PRESET=PATCH|builtin` overrides its template. `--as ID --name NAME`, with one
`--preset`, creates a separately named customization. Replacing an existing
preset ID updates every layout referencing it; review lists those references.

Inspect and edit a saved configuration without a catalog read:

```sh
temper configure --root /private/temper-root --show --json > current.json
```

That response contains `revision` and `configuration`. Save the edited
`configuration` object as `choices.json`, keeping exact locks unchanged when
only changing layouts. Its layout map can contain, for example:

```json
{
  "desk": {
    "name": "Writing desk",
    "presets": ["qwen3.5-4b-q4km-off"],
    "startup": [],
    "default": "qwen3.5-4b-q4km-off",
    "idle_seconds": 600
  }
}
```

Submit the complete configuration with the observed revision:

```sh
temper configure --root /private/temper-root --file choices.json \
  --revision REVISION_FROM_CURRENT_JSON --dry-run --json
```

Omit `--dry-run` to commit. `--remove-preset ID --revision REVISION` removes only
an unreferenced choice. `--resume --json` reports saved exact choices and fresh
machine facts without a catalog/network read. JSON review uses
`temper-configuration-result/v1`; [the data contract](layouts.md) owns schemas.

## Activate, inspect and stop

Activation is explicit and separate from save or prepare:

```sh
temper layout activate desk --root /private/temper-root --listen 127.0.0.1:18080 --dry-run
temper layout activate desk --root /private/temper-root --listen 127.0.0.1:18080
temper layout status --root /private/temper-root
temper layout stop --root /private/temper-root
```

Activation requires prepared inputs. One root has one managed layout. Activating
another switches after checking memory, exact inputs, ownership and active work.
The loopback endpoint accepts included preset IDs and, when selected, `default`.
Stop first to change the listener. Status reports desired and current generations,
router readiness, pending work and observed engines. An engine process being
present does not prove its request readiness.

An explicit launchd job persists after the CLI exits. Temper writes no login
agent and enables no automatic crash restart. The next explicit activation
reconciles a crashed or interrupted job. A small launcher records each engine
lifetime and replaces itself with the engine; it is not a second daemon.
Recorded kernel identity, executable and argv bound all recovery signals.

Switch/stop refuses active requests, unknown process ownership and accepted TCP
connections, conservatively including idle client keepalives. Close idle clients
and retry. There is no force or automatic draining. During the final idle check,
the verified router is briefly suspended; a durable record lets the next mutation
resume that exact lifetime after CLI interruption. Status remains read-only.

Edits, renames or deletion of an active layout do not change its running frozen
generation. Stop remains available even if the layout was deleted. Idle unload
and demand reload are normal managed behavior; Field Kit's
[fixed-lifetime supervisor](execution-runtime.md) remains separate.

## Schema boundary

Configuration files embed execution-lock v3. Recreate pre-preset configurations
from explicit current choices; there is no legacy importer. Historical Field Kit
experiments keep their pinned older host and original inputs.
