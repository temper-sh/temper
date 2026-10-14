# Presets and layouts

`temper init` and `temper configure` edit one selected preset set and named
layouts. The screens are Presets (one tab per catalog group), Layouts, then
Review and prepare. Nothing is selected by recommendation. On first setup, entering Layouts
suggests one editable **Default** layout (`default`) containing every selected
preset, with no startup loading or default model selected. The suggestion is
created once; back navigation preserves edits and deletion. Existing saved
configurations keep their layouts. Users can add, rename or remove layouts.

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

Memory admission uses weights, declared engine caps and exact applicable context
limits when available. It is a prediction, not a measured peak. The startup
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
