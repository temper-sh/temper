# Temper

Temper installs and verifies exact local-AI configurations on Apple Silicon,
so an experiment runs the model files, serving software, and settings it claims
to run.

**Alpha (September 2026):** Temper is usable today by collaborators with a
reviewed configuration file or Field Kit experiment. It is not yet a general
setup tool: the guided model-selection wizard and the first public Field Kit
experiment are still in development.

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

## What to expect

Temper treats a local-AI setup as a reproducible system rather than a loose
collection of model names and command-line flags:

1. A manifest records the configuration the user selected.
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
| System changes | No `sudo`. Temper writes beneath an explicit root and does not silently take over an existing service. |
| Configuration | The user's manifest is never mechanically rewritten after creation. Updates produce explicit lock changes and follow-up commands. |
| Cleanup | Temper removes only state attributed by its own installation receipts and shared-package claims. |

The path exercised with a real model currently uses `llama-server`.
Alternative serving engines—the programs that load a model and answer
requests—can be selected for Rapid-MLX, MLX-VLM, and vLLM-Metal experiments,
but they remain experimental until their exact dependencies and model families
are qualified. In particular, vLLM-Metal is not yet installable through
Temper's managed supply path. See the
[engine and manifest design](docs/design/manifest-schema.md) for the exact
status and refusal rules.

## Temper and Field Kit

[Field Kit](docs/contracts/field-kit.md) is the participant-facing layer. It
will present one bounded question, disclose the exact time, storage, network,
and cleanup effects, record consent, and use Temper for machine facts,
installation, artifact checks, rendering, and an isolated model process.

Field Kit owns questions, sessions, protocols, evidence, reports, and cleanup
choices. Temper owns the stable machine and runtime primitives beneath them.
Keeping the two releases separate lets an experiment improve without changing
the installer, while the exact Temper binary and installed material remain
part of every run's identity.

No participant question is public yet. Until one completes qualification,
installing Temper lets you inspect the machine and use reviewed explicit
workflows, but it does not offer a ready-made model experiment or choose a
setup for you.

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
