# Presets and layouts

`temper init` and interactive `temper configure` follow **Presets → Harnesses →
Review**. Nothing is selected by recommendation. First setup creates one
**Default** layout (`default`) containing every selected preset, with no startup
loading or default model alias. Returning to Presets before saving updates that
initial composition when you continue.

Existing configurations keep every layout and its explicit settings, including
an edited or absent Default. Newly selected presets remain outside those layouts
until placed through `temper layout`; review identifies them. Referenced presets
cannot be deselected in the wizard: it names the affected layouts and directs
membership changes to the standalone layout editor. `temper layout` owns adding,
renaming and removing layouts, membership, startup loading and default routing.

The catalog (`temper-catalog/v1`) names single engine configurations
`presets`. Each preset's `presentation` places it in a group (a tab) and,
optionally, a section within it, with a `description`, optional
`assessment_url` and `memory_tier`. The catalog's groups, their sections and
`preset_order` control presentation within descending memory tiers; they never
select a preset. Presentation is excluded from execution identity. Each saved
preset is stored as an execution-lock v1. Retired catalog and lock formats are
rejected; historical experiments retain their pinned older Temper host. See
the [catalog contract](execution-lock.md#the-catalog).

`temper-configuration/v1` owns a top-level `software` setting (`stable`, the
catalog's tested releases, or `latest`), a selected `presets` map and a
`layouts` map. Each selected preset has a display `name`, its `choices`
(optional `context_window` and `compute`: only what the user set) and its
exact execution `lock`. The lock records its origin catalog preset, so
`temper update` can rebuild it from the newest catalog with the same
choices. One `software` setting covers every preset because a layout runs one
router.
An authored preset also has a `user_preset` file ID. A fork's optional
`fork_baseline` holds its catalog `origin` and a `values` map from overridden
field paths to their previous catalog values. Missing paths are untracked;
JSON null records an unset field. This data supports
[update reporting](init.md#change-your-setup), never execution. Old saved
presets without it remain usable; first placement and full updates capture it.
Each layout has a `name`, `presets` list, `startup` list, optional `default`,
and `idle_seconds` (positive; default 1800). Startup and default independently
reference included presets. Empty layouts and layouts without a default are
valid. Inclusion never implies concurrent residency. Startup is an initial load,
not a keep-loaded policy; idle expiry applies to every preset.

Settings edits create a named customized preset with a newly compiled lock.
They do not inherit context or performance evidence unless the exact settings
still match. Replacing a shared preset explicitly updates every referencing
layout; review lists those layouts. Removing a referenced preset is refused
with the affected layout names until membership, startup and default are
explicitly corrected. Removing a layout never removes installed material.

`temper configure --file FILE [--revision SHA256]` validates and reviews the
complete replacement. Existing roots require the revision returned by
`temper configure --show --json`; first save requires an absent configuration.
The same editor is used by `init` and interactive `configure`. Saves serialize
cooperating writers, check the revision, fsync a staged file and atomically
rename it. Incomplete temporary files are not current state. A canceled or dry
save never writes. Pre-preset configurations must be recreated from explicit current choices.
Saved exact locks support offline resume. Editing and saving never activates.

Preparation deduplicates verified weights by material identity and software by
exact closure, independently of layout membership. Mixed engines and versions
are separate exact inputs. Review counts downloads once and reports startup
memory separately from individual on-demand requirements.
The optional configuration `integrations` array records explicitly selected
[harness destinations](harnesses.md) for Pi, PiG or Hermes. Activation preflights
those files, then synchronizes them after service readiness. JSON activation results include an
`integrations` array with each file's outcome. A file failure returns nonzero
while preserving the successful service state; repeat activation to repair it.

`temper layout activate ID`, `layout status` and `layout stop` operate on one
active layout per root. Activation uses prepared exact inputs. Desired state is
separate from current observations; a failed effect remains pending and the
same command reconciles it. Layout edits, renames and deletion do not change a
running generation. Stop and transitions refuse active requests or uncertain
ownership. There is no force or automatic draining operation. Idle unload and
later demand loading are normal; experiment `execution serve` retains fixed
process lifetimes. All commands accept `--root`; mutating commands accept
`--dry-run`. They say in plain words which layout runs, its base URL, its
model IDs and how to stop it; `--json` prints the status document instead. Service installation on the live stack remains an explicit action.

The activation journal is `ROOT/managed/activation.json` with
`temper-activation/v1`, `desired` and `current` frozen jobs. Each generation
binds router bytes, launch commands, listener and the Temper launch helper's
binary digest. The owned label derives from the absolute root. launchd is
bootstrapped explicitly in the user's GUI domain; no login agent or automatic
crash restart is installed. Activation copies the running Temper binary to
`ROOT/managed/launchers/<sha256>/temper` and launches engines through that copy,
so replacing the installed binary (for example `brew upgrade`) does not break a
running layout. `temper update` or a new activation moves the layout to the
new binary and removes older copies.

An engine launch writes its PID, process group, start identity, exact executable
and argv before exec replaces the launcher. Recorded groups permit cleanup after
router exit; PID reuse ends the old lifetime and never authorizes a signal to
the new process. Unknown children or changed commands refuse recovery. Stop
records a router hold before SIGSTOP, checks accepted TCP connections, queues
TERM and resumes that exact router. This closes the request-admission race;
idle keepalives can conservatively block stop. Interrupted holds resume only on
the next mutation. There is no force signal or background recovery agent.

Memory admission uses an applicable catalog measurement before falling back to
weight-file proxies, supported KV estimates, cache reserves and engine allocation
envelopes. [The shared assessment](memory-estimates.md) identifies the basis,
workload limits and unknown allocations without counting overlapping components
twice. A measured peak does not guarantee use outside its workload. The startup
set must pass together; available alternatives need only pass individually.
CPU and GPU presets both count against physical RAM. GPU presets additionally
count against the wired-memory allowance. The selected device belongs to each
preset and is shared by every layout referencing it; there is no automatic
device fallback when a memory check fails.
Shared software preparation keys on the exact closure digest, not the user
layout name. The managed renderer never merges conflicting engine closures.

The [bounded M5 smoke](../design/managed-layout-smoke.md#observed-result) exercised
mixed engines, idle reload, busy transitions and owned shutdown. Its startup
and cold-cache limits remain explicit; unit recovery tests and the harmless
exec/reload test establish separate mechanical behavior.

## Standalone layout editor

`temper layout` opens the saved layout list; `temper layout edit ID` opens one
layout directly. Both accept `--root PATH` and `--dry-run` and require a terminal.
The existing activate/status/stop commands retain their behavior.

The list offers New, Edit and Delete. A new layout needs a name; Temper derives
an unused ID. Renaming keeps that ID. New layouts start empty, with no preload
or default model and a 1,800-second idle timeout. Empty layouts are valid.
Inside a layout, **Presets** reads the saved files in `ROOT/presets.d` alongside
existing exact presets. A preset saved in the preset editor appears here
without an import or configuration-selection step. Inclusion, preload and
default are independent choices;
preload and default require inclusion. Excluding a preset used for either asks
to clear those references. **Settings** contains the name and idle timeout.
There are no source tabs, catalog browsing or authoring actions. Preset files
resolve against the stored catalog, offline. Invalid files stay visible with
their reason; they do not prevent using other presets. User presets are untested.

Mouse clicks select a layout in the list; Edit opens it. Inside a layout, clicks
toggle preset inclusion, open settings fields and activate breadcrumbs, sections
and footer actions. Preload and Default apply to the selected preset. The wheel
scrolls content. Inputs and confirmations block background controls; disabled
actions and pending work cannot be bypassed by clicking. Back navigation and
membership changes retain the same confirmation guards as keyboard input.

The editor holds one revocable draft. Review shows membership, preload, default
and idle behavior, together with available memory assessment; inability to
assess fit does not prevent saving a structurally valid composition. Activation
retains its machine and preparation checks. Save validates the complete
configuration and publishes it through the existing revision guard. A concurrent
change is refused with the draft retained. Save also captures exact locks and
the source file ID for newly included presets; unchosen files are not saved in
the configuration. A changed or deleted source before its first Save requires
reopening and reviewing it. Existing exact locks stay unchanged, including
when an authoring file is subsequently edited or removed. No automatic merge
occurs. Repeated unchanged saves preserve the saved file. Back and quit offer
Keep editing or Discard when the draft changed. Dry-run Save validates without
writing and retains the draft as unsaved.

Delete confirms the named layout before saving its removal. It preserves all
selected presets, exact locks, integrations, material and other layouts. Deleting
or editing a running layout leaves its running generation intact; activation and
stop remain explicit commands. This editor does not introduce a remembered
current-layout selection while stopped.

## Script preset and layout changes

Select exact catalog presets, optionally customize settings or save one under a
new name. Omitting layout editing creates no implicit layout or default:

```sh
temper configure --root "$HOME/.temper-script" \
  --preset qwen3.5-4b-q4km-off --context qwen3.5-4b-q4km-off=16384 --dry-run --json
```

Omit `--dry-run` to save. Repeat `--preset` for several choices. `--template
PRESET=PATCH|builtin` overrides its template. `--compute PRESET=cpu|gpu`
selects a device offered by that preset. `--as ID --name NAME`, with one
`--preset`, creates a separately named customization. Replacing an existing
preset ID updates every layout referencing it; review lists those references.

Inspect and edit a saved configuration without a catalog read:

```sh
temper configure --root "$HOME/.temper-script" --show --json > current.json
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
temper configure --root "$HOME/.temper-script" --file choices.json \
  --revision REVISION_FROM_CURRENT_JSON --dry-run --json
```

Omit `--dry-run` to commit. `--remove-preset ID --revision REVISION` removes only
an unreferenced choice. `--resume --json` reports saved exact choices and fresh
machine facts without a catalog/network read. JSON review uses
`temper-configuration-result/v1`; the schema above defines the saved configuration.
