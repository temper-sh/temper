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
The header shows your Mac and the date of the catalog in use. A preset this
Temper cannot use is shown disabled with the reason, for example "needs Temper
0.2.0 or newer".

Check the model license, hardware requirements and memory guidance beside your choice.
[Compare the available presets](https://temper-sh.github.io/catalog/) if you need help.
Memory figures are estimates unless a matching measurement is shown.
The size at the top right of each preset card is the total size of its model files.

The underlined license follows the description inline and links to an explanation
or the publisher's terms.
Use your terminal's link-opening gesture, often Cmd-click. Text previews show
the URL explicitly. Licenses come from the selected model files, including
saved custom presets; they are separate from Temper's own software license.

Use **Compute** (`p`) beside Context and Template to switch between CPU and GPU
where the catalog offers both as practical choices. The selected device appears
before Context on the same settings line. Fixed GPU presets omit the device
label; CPU-only presets show **CPU only**. Both omit the Compute control, and
`p` has no effect on them. Reranker settings omit the repeated Reranking label.
The choice belongs to the preset and applies in every layout using it. CPU models
still consume RAM; GPU models also count against the GPU's wired-memory limit.
Changing compute refreshes the memory review and does not inherit performance
or context measurements from the other device.

The template label and control appear only when compatible alternatives are
available; otherwise `t` has no effect.

To explore the wizard without saving or downloading anything, use
`temper init --dry-run`.

## Arrange a layout

A **layout** groups presets for a way of working: coding, writing, or local
helpers alongside a cloud assistant. Your first setup suggests one **Default**
layout containing all the presets you selected. None loads on activation,
and no default model is chosen; models load when requested by their preset ID.

You can continue with this layout or open it to edit these independent choices:

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

## Review and install

Open **Review** to see each model's license, downloads, disk requirements, and
estimated memory use. Model downloads can be many gigabytes; existing compatible
downloads are reused and verified.

Choose **Install and start** to save your choices, install the required files
and start the Default layout. **Save without installing** saves the choices
without downloading; **Cancel** leaves them unsaved. When installation is not
possible, for example because the disk is too full, the first button says so
and the reason appears below the buttons.

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

Temper supplies the local model service; configure the connection in your
preferred app.

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

The same wizard lets you add presets, edit layouts, or change the default.
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
level for a request. Current presets default to medium. Pi's thinking menu
requires a configured local provider; Temper's wizard does not configure Pi.

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

| Key | Action |
|---|---|
| Arrow keys | Move between rows and controls. |
| Enter or Space | Activate a button or toggle a checkbox. |
| Tab / Shift+Tab | Focus the forward / back action; Enter confirms it. |
| Esc | Go back, or cancel the current form. On the first screen it does not leave; press q or choose Cancel. |
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

[Script preset and layout changes](layouts.md#script-preset-and-layout-changes)
or read the [configuration and service reference](layouts.md) for exact behavior.
[Catalog updates](../CATALOG.md) do not replace your saved choices.

Each root carries `temper-root.json`, which records its format. Temper refuses
a root written by a newer Temper, and a root from before 0.1.0; start those in
a fresh root. Keep unfinished Field Kit experiments with the version that
created them.
