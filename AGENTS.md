# Working in Temper

Read `README.md`, `docs/SPEC.md`, and `docs/PLAN.md` before designing or
building anything here. This repository carries the product quality bar:
lab-grade, disposable, machine-specific code does not land here, however well
it worked once.

## Boundaries

- This repository ships reviewed configuration, installers, machine checks,
  and the minimum isolated probe environment. Labs decides and gathers
  evidence; Results explains it; Field Kit independently versions and executes
  frozen portable tests by composing Temper's public primitives.
- Work arrives only two ways: an **accepted product handoff** from Labs
  (`../labs/product-handoffs/`), or **product engineering planned in
  `docs/PLAN.md`**. Never consume moving Labs state, raw experiment output,
  or an unreviewed prototype.
- Do not edit adjacent repositories unless the user explicitly asks for the
  cross-repository step.
- The live machine runs the legacy `local-ai-setup` stack until the release
  cutover gate. Never point the running service, the real manifest, or
  launchd at this repo without the user's explicit go-ahead.
- A recommendation is never consent: no code path may select a model, tool,
  or harness integration the user did not explicitly choose.
- Field Kit discovery, consent, sessions, protocols, evidence, reports, and
  cleanup do not belong in Temper code. Temper carries no Field Kit package,
  session, or protocol runtime. A Field Kit protocol revision must not force a
  Temper release unless it needs a genuinely new, generally useful machine
  primitive.

## Design discipline

The draft craft skill set at `~/work/skills/guild/craft` governs design and
review of product code: `code-organization` (layout; its Go reference
governs the CLI — the whole CLI is Go, decided 2026-08-14), `unit-design`, `data-modeling` (every schema:
manifest, lock, catalog, state), `reliable-effects` (every verb that
mutates), `testing`. Load the matching skill before designing or reviewing;
The design-discipline section of `docs/PLAN.md` maps the set's spine onto
Temper concretely.

Plan-local numbers, milestone labels, decision numbers, and paragraph or
section numbers are never durable names. Outside the plan that defines one,
use the semantic contract or workflow name, a versioned schema/protocol
identity, and a named file or heading link.

The short form of that mapping:

- Every unit is a **pure computation** (render, wall-model arithmetic,
  diffs), a **read** (hardware detection, service status, upstream
  resolution, lease state), or a **side effect** (writing lock/configs,
  launchctl kick, downloads) — never an undeclared mix. CLI verbs are
  orchestrators composing the three.
- Every mutating verb **stages, validates, then commits once**, with
  irreversible effects ordered after the commit. A failure before the commit
  leaves no change.
- **Surface first**: schemas and verb contracts are designed and reviewed
  before implementation; internals stay replaceable behind them (the bash →
  Go oracle strategy depends on this).

## Ground rules (inherited, non-negotiable)

- **Assume yesterday's state is stale** (owner, 2026-08-20). Checking upstream
  versions — engines, models, templates, patches — is routine at the start of
  a session, and the default on finding one is **smoke test and adopt**, not
  defer. Version currency is cheap and reversible; it does not relax the
  evidence bar for any *claim* about quality or speed. For this repo it also
  means a pinned dependency or a hard-coded upstream revision in a design doc
  is assumed rotten until re-checked.
- Anything shell targets **bash 3.2** and is shellcheck-clean: no
  associative arrays, no `${var,,}`, no `mapfile`.
- **Never run `sudo`.** Detect the state, print a ready-to-paste command,
  count it `[manual]`.
- **Second-run-clean** and **`--dry-run` never mutates** are release gates,
  not aspirations.
- `manifest.yaml` is the user's file: written once by the wizard when absent,
  never mechanically rewritten afterward. Advisory diffs only.
- No daemon beyond llama-swap, no background updaters, no telemetry, nothing
  phones home.
- Tests are hermetic and offline; they never touch launchd, the live
  service, or the network. Heavy or runtime verification is on-demand,
  announced first, and needs explicit user authorization.
- The tree is 0BSD and vendors no third-party file; third-party notices ride
  release assets, never the repo.
- Output quality outranks throughput: no model or engine claim on tok/s
  alone; first-attempt task success is the primary metric.
- Do not commit or push unless asked. Preserve dirty worktrees.
