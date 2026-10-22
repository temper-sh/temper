# Your own presets

A user preset is your own combination of engine, weights, template and
settings. Temper compiles, installs, serves and removes it like a catalog
preset, but makes no claim that it was tested: the catalog's presets carry the
evidence, yours carry your experiment.

User presets are plain files in your Temper folder, one file each:

```
ROOT/
├─ presets.d/            your presets (new in Temper 0.3.0)
│  ├─ qwen-cool.json
│  └─ gemma-test.json.off  ignored: only *.json files are read
├─ configuration.json    what you chose in temper init (unchanged)
└─ catalog/ …            the signed catalog
```

Open `temper preset` to create, fork, copy, edit or check your presets. The
editor keeps a draft until you explicitly save it. `temper preset edit ID`
opens an existing file directly. The scripted `fork`, `copy`, `check`,
`weights` and `template` commands remain available, and you can still edit
the files by hand. Build an execution lock with
`temper catalog compile --root ROOT --preset ID`, then use the
[execution commands](execution-runtime.md) to prepare and serve it. In this
version, `temper init`, `temper configure` and `temper update` do not list
your presets.

```sh
temper catalog update --root ~/.temper
temper preset fork qwen3.8-27b-q4xl-mtp qwen-cool --name "Qwen, cooler sampling"
temper preset edit qwen-cool
temper preset check qwen-cool
temper catalog compile --root ~/.temper --preset qwen-cool --target darwin/arm64 --out qwen-cool.lock.json
```

## Two forms: fork and copy

| | Fork | Copy |
|---|---|---|
| The file holds | a catalog preset's ID and your changes | every value of the preset |
| Each compile | starts from the newest catalog preset, then applies your changes | uses your values; only new engine builds arrive |
| Good for | tuning one setting, or other weights of the same model | a new model, or freezing a preset |
| Made by | `temper preset fork ORIGIN ID` | `temper preset copy SOURCE ID` |

Stability comes from the lock either way: a lock never changes, and each
compile writes a new one.

## Interactive authoring

`temper preset` opens the preset manager in a terminal. `temper preset edit ID`
opens one of your files directly. Both accept `--root PATH`, `--memory-gib N`
and `--dry-run`. Without terminal input and output they report how to use the
scripted commands and exit 2; they never launch an external editor. The existing
scripted commands, including their JSON output, keep their behavior.

The manager separates **Yours** from **Catalog**. Yours includes invalid files
with their error, rather than hiding them or preventing other files from opening.
Catalog entries are sources to fork or copy, never editable in place. A new
preset starts from an explicitly chosen catalog preset: fork it for tuning, or
copy it as a starting point for an independent model. Copy also accepts one of
your presets. Choose a name; Temper derives the file ID and adds a numeric
suffix when it is already used. Opening or abandoning this draft creates no
file. IDs stay fixed when editing an
existing file; use Copy for a new ID.

The editor uses **General**, **Sampling**, **Engine**, **Speculation** and
**Review** sections, omitting sections without applicable controls. It exposes
useful user choices from the field table used by `preset fields`; there is no
Advanced category. Model context ceilings, parser identifiers, template
transforms, weights format and fixed engine parameters remain owned by the
source/configuration rather than editable raw fields. Fixed compute choices and
inapplicable generation controls are hidden. Existing metadata and profiles are preserved;
profile settings edit the profile selected for the displayed memory size.
Changing engine families starts from a copy of a preset for that engine.

The shared terminal frame shows `Presets` and the current preset as breadcrumbs,
with editing sections in a centered secondary row. Narrow terminals keep the
selected section visible and mark hidden choices with chevrons. Tab and
Shift-Tab move focus between the content, footer actions, ancestor breadcrumbs
and secondary navigation. Left/right move within the focused navigation or
action row; Enter activates the choice. Returning through a breadcrumb uses
the same unsaved-draft guard as Escape. The compact footer shows shortcuts for
the current context; there is no separate help screen.

Mouse clicks select manager rows, open fields, toggle file choices and activate
visible breadcrumbs, sections and footer actions. Use Open after selecting a
manager row. In a confirmation, clicking a choice selects it; Confirm performs
it. Inputs and confirmations block background controls, and pending reads allow
only cancellation. The wheel scrolls content; clicks follow the visible rows
after scrolling, wrapping or resizing.

- With content focused, up/down move between fields and left/right move between sections. Enter
  opens a field; Enter accepts the input and Escape cancels that input.
- Fork fields mark explicit overrides, even when an override equals today's
  origin. Unmarked values follow the origin; its value is shown for an override. Reset removes
  that override so the next compile inherits again. Copies reset a field to
  its value when the editor opened; they have no implicit catalog inheritance.
- Type errors stay beside the input. Validly typed but incompatible combinations
  may remain in the draft so related settings can be changed together. Check,
  Review and Save use the existing compiler. Invalid drafts cannot be saved.
  A syntactically unreadable existing file can be inspected or deleted in the
  manager; repair its syntax by hand before opening the form.
- Review shows validation, changes, the shared memory assessment and a read-only
  installation plan. Unknown memory and unavailable disk estimates say so.
  Admission refusals do not make an authored preset invalid: it may target a
  different machine. Authoring never establishes quality or speed evidence.
- Saving is explicit. Escape/back or Quit from a changed draft offers **Keep
  editing**, **Discard**, or **Save**; Keep editing is the default. Ctrl-C
  discards the unsaved draft and exits. Successful saves return to the manager.

### Weights and templates in a draft

Weights opens a Hugging Face repository form, then a file picker at
the resolved commit. Only an explicit lookup accesses the network. Loading,
failure, retry and cancellation are visible; existing settings remain usable
offline. Selecting files uses the same size, SHA-256, license and bounded GGUF
header reads as `preset weights`. Small files may be downloaded for hashing;
the editor does not download model weights or install anything. Record IDs are
generated. License metadata is read from the source; missing metadata is
recorded as **Not declared by source**, without blocking local authoring or
implying any license grant. Revision is directly editable; weights format is
derived from the engine. Source identities and license metadata remain in
read-only details.

A fork's replacement weights must be another build of its origin model. The
picker offers that explicit same-model assertion, or **Convert to copy** for a
different model. Conversion freezes the draft's resolved values and records
before changing weights; it is revocable until Save. No same-model assertion
is inferred across repositories. The existing same-repository inference remains.
Copies can select any compatible weights. Template authoring supports the model's
builtin template, a local file in `presets.d`, and a Hugging Face file, using
the existing template helper. The form shows only the fields for that source:
none for builtin, a file for local, and repository/revision/file for Hugging Face.
An existing template's transform stays attached to that source; a different
source does not inherit an unrelated patch. There is no raw transform field.
Choosing a source ignores values retained for inactive sources. Existing record
references remain supported by the file/command interface. These actions change
only the in-memory draft.

Speculation offers configurations already declared for the selected weights
and engine, plus Off where the engine permits it. Selecting a configuration
sets its method, source and draft reference together. Draft-token tuning appears
only when the engine exposes it. The editor couples engine-required values to
the meaningful choice: Rapid reasoning retains/restores its known parser;
Splash reasoning follows its effort selection; vLLM MTP uses one sequence and
no prefix cache. Other authored values remain intact when controls are hidden.

### Save and deletion boundary

Save rechecks the current catalog, compiles the draft and compares the file
with the bytes read when editing began. A changed catalog requires an explicit
**Ctrl-R** to reload and review the draft against it (or reopening the editor);
the reload retains draft edits. A concurrent file edit is never silently
overwritten. New IDs cannot collide with catalog entries or existing files.
Publication stages and syncs one file, then links a new file without replacement
or atomically renames an edited file. Temper writers serialize their comparison
and publication. An external editor that ignores the lock can still race the
last comparison and rename. Unchanged saves preserve the original bytes and
mtime. Cancellation before publication leaves the preset unchanged; a directory
sync failure after publication is reported as saved with durability uncertain.

Save changes only `presets.d/ID.json` (and root housekeeping on the first
write). The layout editor reads that folder, so saved presets appear in its
membership list automatically. Save does not change any layout, rebuild an
existing execution lock, install software, synchronize a harness or restart service.
The manager reports layout references from saved configuration; those layouts
keep their existing locks. Saving a layout captures the exact lock of a newly
included file and its `user_preset` source ID. `temper update` reads that source:
forks follow the catalog origin plus overrides; copies keep their authored values.
A fork's overridden catalog values are retained with its saved configuration,
so [updates report upstream changes](init.md#change-your-setup) while preserving
the authored overrides. A missing or invalid file retains the saved lock and
comparison baseline. `--dry-run` performs validation
and preview but
creates no root, lock file or preset, and cannot delete.

Delete requires an explicit confirmation naming the file and any saved layout
references. It removes only that preset file, after comparing its original
bytes again. Referenced exact locks, shared weights, templates and other files
remain. Repeated deletion of an already absent file is clean. File conflicts
and operational errors retain the draft for correction or copying to a new ID.

## Rules for every file

- The file name is the preset ID: lowercase letters, digits, dots and dashes,
  then `.json`. The file has no ID field. Rename a file to turn it off.
- The file starts with `"schema": "temper-preset/v1"` and has exactly one of
  `origin` (a fork) or `preset` (a copy).
- Reading is strict. An unknown field or an invalid record makes that preset
  unavailable with the reason; your other presets stay available.
- The file can hold weights records (`artifacts`) and template records
  (`templates`) in the catalog's shape. A reference finds the file's record
  first, then the catalog's. When a file record has a catalog record's ID but
  other files, a copy uses its own and `temper preset check` warns; a fork
  refuses it, because a fork changes records only through `changes`, under
  their own IDs. License and display text do not count as other files.
- The engine is always a catalog engine ID. Temper's engine adapters are tested
  with the engine builds the catalog publishes.
- A new preset cannot take a catalog preset's ID: catalog IDs mean one thing.
  If a later catalog adds a preset with your ID, your file is used and Temper
  warns that renaming it shows the catalog's.
- Editing a file never changes a running layout or an existing lock.

## A fork

```json
{
  "schema": "temper-preset/v1",
  "origin": "qwen3.8-27b-q4xl-mtp",
  "name": "Qwen3.8 27B, cooler sampling",
  "changes": {
    "request_defaults": { "sampling": { "temperature": 0.7, "seed": 17 } },
    "engine_config": { "batch_tokens": 1024, "controls": { "threads": 6 } }
  }
}
```

- `origin` names a catalog preset. A fork of your own preset is not possible:
  edit that file, or copy it.
- `name` is optional; the default is the origin's name plus "(yours)".
- `changes` uses the names and nesting of the catalog's preset record and holds
  only what you change. Names joined with dots, such as
  `"request_defaults.sampling.temperature"`, are refused: nest them. `engine_config` and `context_window_tokens` sit at the
  top of `changes`, not inside `profiles`: they change the profile Temper
  selects for the Mac's memory, so a new profile in a later catalog gets them
  too.
- Only the settings in Temper's field table can change. `temper preset fields
  ID` lists them for a preset's engine, with types, choices and current
  values. They are the sampling values and seed, reasoning,
  `max_output_tokens`, speculation, the context window, compute, the weights,
  the template, and every engine setting of the origin's engine.
- Your value replaces the origin's. Nothing can be removed.
- Each compile starts from the newest origin. A catalog change to a setting you
  did not change reaches your preset; your own value wins where both changed.
  A change that becomes invalid makes compile and check fail with the reason.
- A fork cannot change its engine, interface, modalities, profiles as a list,
  excluded engine versions or presentation, other than `name`. A different
  engine or model is a copy.

For llama.cpp chat presets, a fork can also set
`"changes": {"request_defaults": {"reasoning_budget_tokens": 1024}}`.
Use `-1` for unrestricted thinking, `0` for immediate end, or a positive token
count. This optional default requires Temper 0.4.0 and a compatible native
engine/template; it is independent of effort and total output tokens. Explicit
request budgets override it. See the
[reasoning-budget contract](execution-lock.md#settings-and-evidence).

### Other weights of the same model

```json
{
  "schema": "temper-preset/v1",
  "origin": "qwen3.8-27b-q4xl-mtp",
  "name": "Qwen3.8 27B Q6_K / MTP",
  "changes": { "artifact": "qwen3.8-27b-q6k" },
  "artifacts": {
    "qwen3.8-27b-q6k": {
      "same_model_as": "qwen3.8-27b-q4xl",
      "sources": [{ "hub": "huggingface", "repo": "unsloth/Qwen3.8-27B-GGUF", "revision": "<40-character commit>" }],
      "format": "gguf",
      "files": [{ "path": "Qwen3.8-27B-Q6_K.gguf", "bytes": 22900000000, "sha256": "<64 hex characters>" }],
      "license": "Apache-2.0"
    }
  }
}
```

- A fork keeps its origin's model. Other weights are allowed when their record
  says `same_model_as` the origin's weights. Temper cannot verify that claim.
- `temper preset weights qwen-q6 unsloth/Qwen3.8-27B-GGUF Qwen3.8-27B-Q6_K.gguf`
  writes this record and the change. It reads the commit, the license and
  each file's size and SHA-256 from Hugging Face; a small file without a
  SHA-256 there is downloaded and hashed. Weights from the origin's
  repository get `same_model_as` automatically; for another repository, add
  `--same-model-as ID`.
- `same_model_as` exists only in user files. It also lets a template accept the
  weights (see below).
- Known cost: the origin's speculation may depend on its weights, such as MTP
  layers inside them. Compile cannot see that; the first run does. A fork can
  change `speculation` as well.

### Another template

```json
{ "schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": { "template": "builtin" } }
```

- `template` takes `builtin` (the chat template inside the weights), a catalog
  template ID, or the ID of a template record in the file.
- A template accepts weights it lists in `compatible_artifacts`, or weights
  whose `same_model_as` it lists. `builtin` accepts any weights. The lock's
  template record lists the weights it accepted.
- A template record in a file has the catalog's shape. Its `transform` must be
  one Temper knows. Its size and SHA-256 describe the template as prepared,
  after the transform.
- `temper preset template ID REPO FILE` writes a template record from Hugging
  Face and selects it; `temper preset template ID builtin` selects the
  template inside the weights.

### Your own template file

```json
{
  "schema": "temper-preset/v1",
  "origin": "qwen3.8-27b-q4xl-mtp",
  "changes": { "template": "qwen-mine" },
  "templates": {
    "qwen-mine": {
      "sources": [{ "hub": "local" }],
      "files": [{ "path": "qwen-mine.jinja" }],
      "compatible_artifacts": ["qwen3.8-27b-q4xl"],
      "license": "your own file"
    }
  }
}
```

- The `.jinja` file sits next to the preset in `presets.d`. Its name is a plain
  file name, not a `.json` file, so it is never read as a preset.
- A local template has no `bytes` or `sha256` in the preset file. Each compile
  reads the file, applies its `transform` and records the size and SHA-256 in
  the lock. Edit the file, compile, get a new lock.
- `prepare` reads the file from the same root and needs no network for it. A
  file edited after the compile stops `prepare` until you compile again, so a
  lock never serves a template other than the one it names. A lock with a
  local template prepares only in a root that has the file.
- `temper preset template ID --local FILE.jinja` writes the record and selects
  it.

## A copy

```json
{
  "schema": "temper-preset/v1",
  "preset": {
    "artifact": "qwen3.8-27b-q4xl",
    "template": "froggeric-qwen38",
    "engine": "llama-cpp",
    "interface": "chat-completions",
    "modalities": ["text"],
    "compute": "gpu",
    "compute_options": ["gpu"],
    "profiles": [{ "min_memory_gib": 32, "context_window_tokens": 65536, "engine_config": { "kind": "llama-server/v1", "…": "…" } }],
    "presentation": { "display_name": "My Qwen" },
    "…": "the rest of the catalog's preset record"
  },
  "artifacts": { "qwen3.8-27b-q4xl": { "…": "the catalog's weights record" } },
  "templates": { "froggeric-qwen38": { "…": "the catalog's template record" } }
}
```

- `preset` is exactly a catalog preset record, with the catalog's validation,
  so moving a copy into the catalog is a copy and paste, plus evidence.
- `temper preset copy` writes one profile: the one Temper selects for this
  Mac's memory, or for `--memory-gib N`. It copies from a catalog preset or
  from one of yours; copying your own fork freezes it.
- Every weights and template record the preset uses is in the file: the
  weights, the projector and the draft weights where the preset has them, and
  the template. A copy keeps its meaning when the catalog changes or drops
  those records.
- `template` is optional, as in the catalog. For a new model, change
  `artifact` and its record, and add the weights to the template's
  `compatible_artifacts` or use `builtin`.
- `presentation.display_name` is required.
- `exclude_engine_versions` works as in the catalog. A copy takes new engine
  builds, so this is how you block one that breaks it.

## Commands

The scripted commands below take `--root PATH` (default `~/.temper`) and `--json` for one JSON
document. They read the root's active catalog, so run `temper catalog update
--root PATH` first. The revision defaults to `main` and is recorded as its
exact commit. `HF_TOKEN` is used for gated repositories. `weights` and
`template` change a file only when the result still compiles.
For GGUF weights, the helper also reads a bounded header from the pinned file
to obtain supported attention-cache geometry. Unsupported or missing metadata
is reported as unknown; no full weight download is needed.

`check` and setup share the [memory assessment](memory-estimates.md).
`memory_estimate.basis` identifies a matching catalog measurement or an estimate.
Measurements retain loaded/peak values and workload limits; changing the tested
execution or machine falls back to estimation. Estimates separate KV payload,
weight-file storage proxies, cache reserves, engine envelopes and unknowns.
`fits` is omitted when total
runtime fit is unknown and is `false` when admission refuses the preset;
weights alone cannot produce `fits: true`.

| Command | What it does |
|---|---|
| `temper preset fork ORIGIN ID [--name TEXT]` | Writes `presets.d/ID.json` with the origin and no changes yet. |
| `temper preset copy SOURCE ID [--name TEXT] [--memory-gib N]` | Writes a copy of a catalog preset or of one of yours. |
| `temper preset check [ID] [--memory-gib N]` | Reads and compiles one preset, or every file in `presets.d`. It shows a fork's changes, the build, memory estimates and unknowns on this Mac, and warnings. It exits 1 when a preset is invalid; a preset refused by memory admission is still valid. |
| `temper preset fields ID [--memory-gib N]` | Lists the settings a fork can change for that preset's engine, with current values. |
| `temper preset plan ID` | Reviews installing the preset on this Mac the way `temper init --dry-run` reviews catalog presets: the files to download, those already in the Hugging Face cache or in Temper, the disk needed and any refusal. It saves and downloads nothing. With `--json` the document is `temper-preset-plan/v1`, whose `plan` has the same fields as `init --json`'s. Added in 0.3.1. |
| `temper preset weights ID REPO[@REVISION] FILE…` | Adds a weights record from Hugging Face and selects it. File names can be patterns, such as `'*.safetensors'`. In a copy, it also adds the weights to the copy's template. |
| `temper preset template ID (REPO[@REVISION] FILE \| --local FILE.jinja \| builtin)` | Adds a template record from Hugging Face or for your own file and selects it, or selects the template inside the weights. |
| `temper catalog compile --root ROOT --preset ID …` | Compiles your preset when `presets.d` has it, else the catalog's. With `--json` the report adds `user_preset` and `warnings`. |

The lock of a user preset is an ordinary execution lock (`temper-execution-lock/v1`)
named by your ID. Its hash identifies the exact execution, as for any lock.

## Compatibility

`temper-preset/v1` files are part of Temper's compatibility promise from 0.3.0:
later Temper versions read them.

Not in this version: a fork of a fork or of a copy, removing a setting in a
fork, values per memory size in a fork, a record of where a copy came from,
and engine records in user files.

[Back to the command reference](README.md)
