# Temper

Temper installs and verifies exact local-AI configurations on Apple Silicon,
so an experiment runs the model files, serving software, and settings it claims
to run.

**Alpha (September 2026):** choose catalog presets, compose named layouts, or run
a reviewed Field Kit experiment. Independent Field Kit observations are pending;
the linked evidence describes the configurations exercised so far.

## Why this is useful

A local model setup can drift without looking broken. A model name can resolve
to new bytes, an engine update can change a default, or a Python tool can pick
up whatever happens to be installed on the machine. The server may answer while
running a materially different configuration from the one that was tested.

Temper makes those details explicit. It locks model and software files by
cryptographic hash, installs isolated runtimes, renders commands for the
selected serving software, checks the machine before startup, and refuses
states it cannot verify. Recommendations remain separate from consent: Temper
never chooses or installs a model, tool, or integration that the user did not
select.

The 2026-09-02 llama-server acceptance run demonstrates that boundary with a
real artifact. Temper reverified a locked 17.1 GB model, started it so only the
test Mac could reach it, used a 24,576-token context with the intended memory
and reasoning controls, received a valid response, and shut it down cleanly.
This establishes that the exact configuration runs; it is not a universal
model-quality or hardware claim.
[Read the acceptance record](docs/acceptance/current-posture-render.md).

## Try it

### Install the current alpha

Temper releases are signed and notarized for macOS on Apple Silicon. Download
the ZIP and matching checksum from the
[Temper Releases page](https://github.com/temper-sh/temper/releases):

```text
temper_VERSION_darwin_arm64.zip
temper_VERSION_darwin_arm64.zip.sha256
```

Verify and install without `sudo`:

```sh
cd ~/Downloads
shasum -a 256 -c temper_VERSION_darwin_arm64.zip.sha256
unzip temper_VERSION_darwin_arm64.zip
mkdir -p "$HOME/.local/bin"
install -m 0755 temper_VERSION_darwin_arm64/temper "$HOME/.local/bin/temper"
"$HOME/.local/bin/temper" version
```

Replace `VERSION` with the version shown on the release. Add
`$HOME/.local/bin` to `PATH` if you want to invoke `temper` without its full
path.

### Inspect your Mac

The smallest useful command is a read-only machine inspection:

```sh
temper machine facts
```

It reports the stable hardware and memory facts used by compatibility and
safety checks. It does not install software, download a model, or change the
machine.

If you already have a reviewed Temper manifest and lock, continue with
[the explicit configuration workflow](docs/EXPLICIT-WORKFLOW.md). It keeps
resolution, downloads, rendering, checks, and activation as separate visible
steps.

### Choose a catalog configuration

Alpha.11 compiles catalog presets to exact execution-lock v3 inputs.
[Browse and use the signed catalog](docs/CATALOG.md). Upgrading from an older
alpha requires a fresh Temper root; see the
[upgrade boundary](docs/releases/0.1.0-alpha.11.md#upgrade-boundary).

### Preview guided setup

The wizard lets you select presets, compose named layouts, and review
what will be saved or downloaded:

```sh
temper init --dry-run
```

A **preset** combines model weights, an engine and its settings. Recommended
contains five curated choices; All also includes the smaller catalog choices.
Nothing starts selected. Model, Weights and Engine remain visible separately,
alongside each recommendation's description and the machine checks.

A **layout** combines your selected presets. Local and Utility are editable
starter names. Choose which presets are available, which load on activation,
and an optional default for clients. These choices are independent; installing
alternatives does not require loading them together. Context and template edits
belong to the preset and affect every layout that uses it.

Save writes exact choices to `~/.temper/configuration.json` by default. Prepare
installs missing material, reuses exact software and weights, and starts no
service. Later, `temper configure` opens the same editors. Explicit
`temper layout activate NAME`, `layout status`, and `layout stop` manage one
active layout per root. Idle models can unload and reload on demand. This
managed launchd path is available in alpha.11. Its
[bounded M5 smoke](docs/design/managed-layout-smoke.md#observed-result) exercised
mixed engines, idle reload and owned shutdown. Splash's first cache conversion
can exceed a short client timeout, and startup remains subject to memory checks.
It does not change the running legacy service.

[Set up and manage presets and layouts](docs/contracts/init.md) covers scripted
editing, offline resume, activation and recovery. The
[authoring catalog guide](catalog/README.md) links measured configurations and
limitations. Signed sequence 3 contains its five recommendations.
Memory figures are predictions
unless tied to exact applicable evidence; 8/16 GiB machine fit remains
unmeasured.

Preparation checks the shared Hugging Face cache and verifies reused weights.
Missing weights use `hf`, through `uv tool run` when needed. Temper never prunes
that shared cache. [Cache and support-tool behavior](docs/contracts/fetch.md#shared-hugging-face-cache).

## What to expect

Temper treats a local-AI setup as a reproducible system rather than a loose
collection of model names and command-line flags:

1. A user-owned configuration stores selected preset locks and named layouts.
2. Lock files identify the exact model, template, engine, Python runtime, and
   dependency artifacts needed for that configuration.
3. Temper verifies those artifacts, predicts whether the models kept in memory
   will fit, and renders commands through a dedicated adapter for each serving
   engine.
4. An isolated probe can start the reviewed configuration so it is reachable
   only from the same Mac. Receipts make later checks and cleanup attributable
   to the same installation.

The current release target and safety boundary are deliberately narrow:

| Concern | Current behavior |
|---|---|
| Machine | macOS on Apple Silicon; the published binary is `darwin/arm64`. |
| Downloads | Model fetches and runtime installation are explicit and may use many gigabytes. Dry runs do not mutate. |
| Privacy | No telemetry or background updater. Model serving listens only on the Mac itself; retained evidence stays local unless a person chooses to export it. |
| System changes | No `sudo`. Temper owns files beneath an explicit root; source preparation also uses the shared HF/uv caches for model downloads and their support tool. It does not silently take over an existing service. |
| Configuration | User choices are never mechanically overwritten. Setup saves exact preset locks and layouts in configuration.json; manifest v2 remains available. |
| Cleanup | Temper can remove its receipted private installations. It never removes system-managed packages, including packages it requested. |

Native checks have exercised llama.cpp and Splash on the reviewed Macs.
Alternative serving engines—the programs that load a model and answer
requests—can be selected for Rapid-MLX, MLX-VLM, and vLLM-Metal experiments,
but they remain experimental until their exact dependencies and model families
are qualified. The experimental Qwen catalog includes exact Python closures for
Rapid-MLX and vLLM-Metal. See the
[engine and manifest design](docs/design/manifest-schema.md) for the exact
status and refusal rules.

## Temper and Field Kit

[Field Kit](docs/contracts/field-kit.md) is the participant-facing layer. It
presents one bounded question, discloses the exact time, storage, network,
and cleanup effects, records consent, and uses Temper for machine facts,
installation, artifact checks, rendering, and an isolated model process.
For experiments that require an exact rendered token count, Field Kit can also
ask Temper to run the tokenizer from the receipted llama.cpp installation
against the manifest-locked model. Temper returns token IDs; Field Kit still
owns prompt construction, probe placement, grading, and search policy.

Field Kit owns questions, sessions, protocols, evidence, reports, and cleanup
choices. Temper owns the stable machine and runtime primitives beneath them.
Keeping the two releases separate lets an experiment improve without changing
the installer, while the exact Temper binary and installed material remain
part of every run's identity.

Temper supplies the execution-lock commands required by the
[Field Kit collaborator alpha](https://github.com/temper-sh/field-kit).
Its experiments require an explicit experiment plan and machine-owner
consent. No question has completed external-machine qualification yet.

The [direct execution commands](docs/contracts/execution-runtime.md) consume
execution-lock v3 inputs directly and return supervised process
identities and shutdown results. Field Kit's revision 4 study pins the published
alpha.11 host. Older studies retain their producing hosts and frozen inputs;
catalog updates do not change them.

## Learn more

- [Run an explicit configuration](docs/EXPLICIT-WORKFLOW.md) — resolve, fetch,
  verify, render, and inspect a reviewed manifest without touching a live
  service.
- [Understand the product and safety model](docs/SPEC.md) — intended users,
  consent, reproducibility, machine fit, and configuration principles.
- [Audit the current llama-server witness](docs/acceptance/current-posture-render.md)
  — exact conditions, observed behavior, and limits of the claim.
- [Read the command contracts](docs/contracts/) — precise behavior for apply,
  fetch, update, software installation, Field Kit binding, and probing.
- [Follow development](docs/PLAN.md) — remaining work, acceptance gates, and
  recorded design decisions.
- [Contribute to the Go code](docs/CODE.md) — package map, effect boundaries,
  tests, and release tooling.

## License

[0BSD](LICENSE). The source tree vendors no third-party files; required notices
are generated into release assets from the exact linked dependency graph.
