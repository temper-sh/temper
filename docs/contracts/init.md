# Set up and use your local models

This guide takes you from choosing a preset to using it in a chat or coding app.
[Install Temper](../../README.md#get-started) first.

## Choose a preset

Open the setup wizard:

```sh
temper init
```

A **preset** combines model weights chosen for quality, an engine suited to the
model and hardware, and tuned settings. **General** holds recommended
general-purpose presets. **Specialist** groups recommended specialists by task,
with explanations of their purpose and tradeoffs. **Other** contains the
remaining choices, including smaller models and saved presets no longer in the
catalog. Each preset appears in one tab. Nothing is selected for you.
The wizard follows **Presets → Harnesses → Review**. Machine and catalog details
appear with the preset choices. A preset this Temper cannot use is shown
disabled with the reason, for example "needs Temper
0.2.0 or newer".

Check the model license, hardware requirements and memory guidance beside your choice.
[Compare the available presets](https://temper-sh.github.io/catalog/) if you need help.
Memory figures are estimates unless a matching measurement is shown.
The size at the top right of each preset card is the total size of its model files.
The memory review separates weights, supported context-cache estimates and
unknown allocations. A weights-only estimate does not establish runtime fit.
See [Memory estimates](memory-estimates.md) for supported caches and limits.

The underlined license follows the description inline and links to an explanation
or the publisher's terms.
Use your terminal's link-opening gesture, often Cmd-click. Text previews show
the URL explicitly. Licenses come from the selected model files, including
saved custom presets; they are separate from Temper's own software license.

The **Context** action or `c` opens a separate context prompt. **Apply** updates
the draft and memory assessment; **Cancel** or Escape retains the previous
value. Invalid input remains in the prompt until corrected or cancelled. Preset
cards keep their context summary. For an included preset offering both devices,
the **Compute** action or `p` opens a separate CPU/GPU prompt at the current
device. Left/Right, Space or a click changes the pending choice; **Apply**
updates the draft and memory assessment. **Cancel** or Escape keeps the previous
device. The selected device appears
before Context on the same settings line. Fixed GPU presets omit the device
label; CPU-only presets show **CPU only**. Both omit the Compute control, and
`p` has no effect on them. Reranker settings omit the repeated Reranking label.
The choice belongs to the preset and applies in every layout using it. CPU models
still consume RAM; GPU models also count against the GPU's wired-memory limit.
Changing compute refreshes the memory review and does not inherit performance
or context measurements from the other device.

When a preset uses a custom chat template, its name appears in the settings.
Use `temper preset` to edit an authored preset's template.

To explore the wizard without saving or downloading anything, use
`temper init --dry-run`.

## Your initial layout

A **layout** groups presets for a way of working: coding, writing, or local
helpers alongside a cloud assistant. Your first setup creates one **Default**
layout containing all the presets you selected. None loads on activation,
and no default model is chosen; models load when requested by their preset ID.

Layout editing lives in `temper layout`. After setup, use it to change these
independent choices:

| Choice | What happens |
|---|---|
| Included | The model is available to your apps. |
| Load on activation | The model loads when you start the layout, saving a wait on first use. |
| Default | Apps can request `default` instead of the preset's ID. |

A layout can include several models without keeping all of them in memory.
llama-swap loads models when requested and unloads idle ones, after 30 minutes
by default. Models that cannot fit together take turns. The models you choose
to load on activation must fit together.

You can edit, rename or remove **Default**, or add another layout, such as
**Helper** for small models. The same presets can be reused in several layouts.

## Connect a harness

On **Harnesses**, select Pi, PiG or Hermes if you want Temper to keep its model
picker in sync with the active layout. Detection never selects an app for you.
Review the displayed configuration destination (`models.json` for Pi/PiG,
`config.yaml` for Hermes); it is saved with your choices.
Pi and PiG may share a file, in which case both see the entries.

Activation updates the included chat models, including models loaded on demand.
Saving alone does not change a harness file. Your app's default model, session
and credentials stay under your control. See [Harness integrations](harnesses.md)
for ownership, directory overrides, deselection and retry behavior.

## Review and install

**Review** confirms selected models and harnesses, layout availability,
download/disk totals and a short memory assessment. Software totals include every
artifact in each exact installation closure, including managed CPython, wheels
and source archives. Identical closures share one installation; verified receipts
and read-only provider inspection credit existing native and Python installations.
Python disk estimates reserve the runtime's 2 GiB extraction limit and four times
each package's compressed size (at least its declared source size), with two
expanded copies for build/install staging, plus retained downloads. These are
planning allowances, not measured installed sizes or guaranteed build bounds.
The Downloads table shows
individual files directly. Model licenses and preset settings remain on the
selection screen and in the preset editor. Cached files are reused and verified.

Choose **Install and start** to save your choices, install the required files
and start the Default layout. **Save without installing** saves the choices
without downloading; **Cancel** leaves them unsaved. When installation is not
possible, for example because the disk is too full, the first button says so
and the reason appears in the review.

## Activate, inspect and stop

After installation the Default layout is running. To start it again later,
for example after restarting your Mac:

```sh
temper layout activate default
```

In your chat or coding app, use:

| Setting | Value |
|---|---|
| OpenAI-compatible API base URL | `http://127.0.0.1:8080/v1` |
| Model | An included preset's ID, or `default` if you selected a default model in the layout |

For a selected Pi integration, open `/model` to refresh its picker. Restart PiG
if its picker has not refreshed. For Hermes, start a new session and choose
`temper` in its model picker. Other apps can use the connection above.

```sh
temper layout status
temper layout stop
```

Activation keeps the service running after the command exits. Start it again
after restarting your Mac; it does not start automatically at login.

## Change your setup

```sh
temper configure
```

The same three-step wizard lets you select presets and harness integrations.
Reopening it preserves every existing layout, including its membership,
preload, default-model and idle choices. It does not recreate a missing Default.
Review names models outside layouts; place them
with `temper layout`. Referenced presets cannot be deselected until their
memberships are changed in the layout editor; the wizard names those layouts.
During an unsaved first setup, returning to Presets updates the initial Default
to match the selections. Cancel and dry runs save nothing. Scripted
`configure --preset` retains its existing explicit selection contract.

Review and install any missing files; the running layout then restarts with
the changes. Saving alone does not change a running service.

To get the newest presets and software for the presets you saved:

```sh
temper update --dry-run   # show old → new for each saved preset
temper update             # save, install and restart the running layout
```

`temper update` downloads the newest catalog and rebuilds each saved preset
from its catalog preset, keeping your context and compute choices. A preset
that left the catalog, needs a newer Temper or no longer fits keeps its models
and settings and is reported as `kept` with the reason; it still gets the
newest router, because a layout runs one router. `temper update --software`
changes only the router and engine releases and keeps everything else; it
notes when the result is not a combination the catalog tested.

Presets placed from `presets.d` retain their source file ID. Full updates read
that file: forks combine the current catalog origin with authored overrides;
copies retain their authored values and use the current software. Your saved
context and compute choices remain. If the file is missing, invalid or no
longer fits, the entire working lock stays unchanged and the reason is reported.
`--software` keeps the saved model and settings even when authoring is unavailable.

For a fork, updates also report catalog changes to fields it explicitly overrides:
the field path, previous and current catalog values, and the authored override.
The override stays in place. The comparison uses the catalog values saved with
the preset, so a separate catalog refresh cannot erase it. Only overridden
fields are retained, using the origin profile selected for this Mac; this is
comparison data, not another execution lock.

First layout placement captures that baseline. A successful full configuration
save advances it, even when overrides leave the execution lock unchanged; a
comparison-only save installs and restarts nothing. Dry runs, refused rebuilds
and `--software` preserve it. New overrides, a different origin, and older saved
presets without a baseline are reported as untracked until the next full save;
Temper does not infer their previous catalog values from the fork's lock.
Copies have no upstream override comparison. Removing an override resumes
inheritance and removes its baseline on the next full save.

`--json` adds `override_changes` to each preset result, with `path`, `from`,
`to` and `override` values, and `untracked_overrides` for paths without a prior
comparison value. JSON `null` means the catalog field was unset, distinct from
zero or false. A post-save preparation failure still emits the report, with
an `error` in JSON and a nonzero exit; the saved configuration can be resumed.

`temper configure --software latest` follows the newest upstream router and
engine releases for every saved preset; `--software stable` returns to the
releases the catalog tested. When a newer release cannot be used, Temper keeps
the tested one and prints why.

For example, after creating and preparing a Helper layout:

```sh
temper layout activate helper
```

This switches from the active layout after checking that the current requests
have finished. Close idle client connections if they prevent switching.

Context controls how much conversation or source material a model can use.
Larger settings can need more memory and take longer to answer. Keep the
preset's setting unless you have a reason to change it; its advertised maximum
does not establish fit on your Mac. Context and compute edits affect every
layout using that preset, and `temper update` keeps them.

Clients that support the engine's request controls can change the thinking
level for a request. Current presets default to medium. See
[harness integrations](harnesses.md) for Pi, PiG and Hermes behavior.

## Model storage

Source weights stay in the shared Hugging Face cache, normally
`~/.cache/huggingface/hub`, honoring the standard HF environment settings.
Temper stores references, templates and verification metadata under its root.
It does not copy weights when the cache is on another disk. There is no separate
model-location control in the wizard yet.

Cache cleanup can remove files used by your presets. Preparing again downloads
missing files and verifies changed files. Splash may also build its own derived
native runtime cache under the Temper root on first use.

## Recover from a problem

| Problem | What to do |
|---|---|
| Preparation was interrupted | Run `temper configure --resume --prepare`. It reuses your saved choices and completed downloads. Missing files still need network access. |
| HF cache files were removed or changed | Run `temper configure --resume --prepare` to download missing files and verify the cache again. |
| A model does not fit | Choose a smaller preset, reduce context, or load fewer models on activation. An unknown estimate is not a measured fit. |
| First Splash startup takes a long time | Its first run builds an additional weight cache. Allow time and disk space for conversion; a short client timeout may expire first. |
| Switching or stopping is refused | Let active requests finish, close apps holding connections, then retry. |
| The service crashed | Retry `temper layout activate default` to recover the saved layout. If Temper cannot verify process ownership, keep the reported details for diagnosis. The router log is `~/.temper/managed/logs/router.log`. |
| The HF cache moved | Run `temper configure --resume --prepare`. Temper links the models from the cache you configure now; it never writes into the old one. A cache on a disconnected disk is refused until you connect it. |
| The Temper folder was moved or copied | `layout stop` prints the `launchctl bootout` command that stops the old job. A folder cannot move; start again with `temper init`. |
| Temper refuses a folder "from a Temper before 0.1.0" | Move it away or delete it, then run `temper init`. |

Deleting a layout leaves its models installed. To remove a preset from your
selection, first remove it from layouts that use it.

## Keyboard controls

The wizard follows the same [terminal controls](terminal-ui.md) as the preset
and layout editors, including arrow focus and scrolling. Context and Compute
editing use focused prompts with Apply and Cancel.

| Key | Action |
|---|---|
| Arrow keys | Move between rows and controls. |
| Enter or Space | Activate a button or toggle a checkbox. |
| Tab / Shift+Tab | Move focus between content, actions, primary navigation and category filters; Enter activates the focused control. |
| Esc | Go back. On the first screen it does not leave; choose Cancel or press q outside text input. |
| PgUp / PgDown | Scroll long cards and reviews. |

Mouse clicks and trackpad scrolling also work. On the preset screen, the first
letter of a tab's name opens it (for example `g` for General) when no other tab
shares that letter. Up from the first row focuses the tabs; Left/Right changes
the tab and Down enters its choices. `c` edits context, and `p` switches
between CPU and GPU where both are offered.

## Configuration and advanced use

Choices are saved in `~/.temper/configuration.json`. Use `--root PATH` on setup
and layout commands to keep another independent configuration, for example:

```sh
temper init --root "$HOME/.temper-writing"
temper layout activate default --root "$HOME/.temper-writing"
```

Each root has one active layout. To use a different local port, stop that
layout first, then activate it with `--listen 127.0.0.1:18080`.

Each root's engines use their own block of 32 local ports, so two roots can
run at the same time: `~/.temper` starts at port 10001, and every other root at
a block chosen from its folder path. When nothing from the root runs, `layout
activate` stops if another process already listens on one of those ports, and
names it.

[Script preset and layout changes](layouts.md#script-preset-and-layout-changes)
or read the [configuration and service reference](layouts.md) for exact behavior.
[Catalog updates](../CATALOG.md) do not replace your saved choices.

Each root carries `temper-root.json`, which records its format. Temper refuses
a root written by a newer Temper, and a root from before 0.1.0; start those in
a fresh root. Keep unfinished Field Kit experiments with the version that
created them.
