# temper — product spec

Adopted 2026-08-14 as this repository's working product spec. Drafted
2026-08-07 in `local-ai-setup/docs/TEMPER.md` by the working session that ran
the field kit's first live probe; extended through the 2026-08-13 owner
discussions (org shape, wizard universe, workflow modes, harness
integrations, home directory and daemon control). The source draft carries a
moved-note and remains as drafting history. References of the form "PLAN §N"
and "FINDINGS #N" resolve in the legacy `local-ai-setup` repo — the evidence
history Temper Labs will archive; items marked **proposed** still await owner
adjudication.

Naming (settled): the binary is `temper`, the org is `temper-sh`. This
repository is `temper-sh/temper`, the release repo — built clean, it takes
the name. The evidence side is `temper-sh/labs` (a fresh scaffold created
2026-08-14 at `../labs`; the legacy `local-ai-setup` repo and its history
remain the source of existing evidence until Labs' archive migration imports
it). `temper-sh/results` is the human-readable evidence publication, created
locally 2026-08-13; `temper-sh/field-kit` already exists and is absorbed as
the probe surface (below).

## One paragraph

temper installs, tunes, and verifies a local-LLM stack on a Mac — and
refuses to pretend. Every recommendation traces to a Labs decision packet, a
measurement on real hardware, and a concise Results record people can audit
without reading the lab journal; every number carries the conditions it ran
under, and anything unmeasured is labeled unmeasured. The wizard offers a
curated, tested universe of models and individually opt-in tools. Workflow
modes activate subsets of that universe; they never smuggle in an unselected
model, tool, or harness integration.

## Users and their jobs

1. **The owner-operator** (the stranger installing): wants a working local
   stack matched to their hardware without reading a lab notebook. Runs
   the wizard once, gets a manifest they own, applies updates when they
   choose.
2. **The friend with a different Mac**: runs the probe (today's
   field-kit), sends back one text block, gets to keep or fully remove
   the result. Their machine's witnessed combination can become a catalog
   row.
3. **The AI agent driving either of the above**: first-class. Stable
   RESULT lines, machine-parseable outcomes, an interpretive runbook
   (AGENT.md's evidence model), consent gates that stay human, and a
   `conclude` channel so its analysis lands in the artifact, not a chat
   log.

## Principles (inherited, non-negotiable)

- **Measured beats plausible.** A catalog row ships only after Labs produces
  a reviewable qualification packet and a witnessed run (machine, SHAs, date,
  conditions, corpus and numbers). A mechanical probe can rule a combination
  *out*, never establish model or tool quality by itself.
- **Every tool is explicit.** Tools start unselected. Recommendations explain
  why a tool fits, but installation requires a one-by-one affirmative choice. A
  mode or profile may only narrow the selected universe; it cannot add a tool,
  install an integration, or widen a data boundary.
- **Nothing phones home.** Reports are local files a human chooses to
  paste. No telemetry, ever. (The non-local helper group sends *inference
  requests* to a provider the owner configured — that is the owner's data
  boundary decision, stated on the row, and distinct from telemetry.)
- **The user's manifest is theirs.** Written once by the wizard, then
  never mechanically rewritten (ground rule 6). Advisory diffs only.
- **No sudo, ever.** Privileged tweaks are printed for the human.
- **Conditions on every number.** wall / swap / tune label / thermal /
  power / load — measurements without conditions are anecdotes.
- **Second-run-clean.** Idempotence is a release gate, not a nice-to-have.

## Configuration model

A machine's configuration is a combination across seven dimensions:

1. selected model universe · 2. selected tool universe · 3. installed harness
integrations · 4. workflow-mode bindings · 5. per-role engine and tuning ·
6. residency strategy · 7. local/cloud routing and fallback.

The bucket axes — RAM × chip generation × memory bandwidth — select a
*starting* combination (a prior, not a SKU). The wizard proposes it; the
probe witnesses it; the witnessed combination is what the machine runs.
The wall model (fraction × Metal device memory + co-tenants + OS floor ≤
wired limit) is the arithmetic the tool uses to gate proposals before any
measurement, and `gpu-budget` output is always labeled a *prediction*.

The wizard writes the first three dimensions from explicit choices. A mode
template proposes the fourth through seventh, clipped to those choices. The
manifest and lock preserve both the user's selection and the exact qualified
profile revisions used to make it.

## Wizard set and profiles (settled 2026-08-13)

The wizard is a choice among curated and tested artifacts, not an open-ended
model browser. It proceeds in this order:

1. detect hardware and allowances;
2. choose models that may be used on this machine;
3. choose tools individually, with backend, data-boundary and dependency
   consequences shown before selection;
4. choose which already-installed harnesses receive Temper integrations;
5. choose or edit workflow-mode templates built only from steps 2–4;
6. preview downloads, residency, mode transitions and external data paths;
7. write the manifest once, then render and probe it.

Several large models may be installed and available. By default only one large
local model is resident or serving at a time; a mode switch unloads before it
loads another unless a machine-specific witnessed profile explicitly permits
co-residency. Small specialists remain on demand or CPU-placed according to the
mode's witnessed resource profile.

**Profile** has one precise catalog meaning: a versioned, evidence-backed
configuration record. Profiles may exist for model artifacts and runtimes,
engines, tools, harness adapters or complete modes. A profile declares
compatibility, defaults, dependencies, data boundaries, resource placement,
known failures, regression suite,
evidence status and witness scope. A profile is advice and configuration, not
consent. In particular:

- a **model artifact profile** pins weights, the complete quantization recipe
  (format, layer/tensor precision map, calibration provenance and sidecars),
  tokenizer, template and immutable provenance once;
- an **engine profile** pins the executable/package, full dependency closure,
  API/capability surface, process isolation, service contract and known
  engine-wide failures; it is a dependency record, not a user-facing tool;
- a **model runtime profile** references an artifact and engine profile and pins
  placement, context/KV settings, sampling/thinking, batching, speculation,
  residency, preload and TTL for a machine bucket and mode;
- a **tool profile** pins the tool core, transport, schema/description,
  backend role, permissions and harness/model affordance deviations;
- a **mode profile** composes qualified artifact/runtime and tool profiles for
  a job, and may override runtime configuration per selected model;
- an **activity profile** such as inspect/change/verify/review narrows the
  active tools inside a mode and never widens them.

Runtime profiles are deliberately plural for one artifact. On the same 32GB
machine, a reranker may use `-ngl 0` and short TTL in coding mode so the large
coder owns the GPU, then use GPU placement and a longer TTL in research or
helper mode after that coder unloads. A primary model may likewise have
different context caps, KV precision, batching, speculation, thinking,
sampling or memory fractions in planning and coding. The lock deduplicates the
artifact download but pins each runtime-profile revision and rendered-config
hash separately.

The current field-kit bridge serializes an exploratory runtime witness as a
`field-kit-runtime-profile/v1` packet: exact packet, evidence and (when the
normal stack can execute it) manifest hashes are verified, and the packet
identity is stamped into runtime measurements only after the selected manifest
is active; preflight labels it pending. A tune invalidates the identity.
An `external-lab` packet is inspectable but cannot enter the generic install
path. This is the Labs-to-probe handoff format, not a premature decision about
the release catalog's normalized schema and never a consent token.

Compatibility and deterministic template results may be reused when the
artifact is identical. Fit, stability, cache and performance witnesses may not:
their evidence key includes artifact revision, engine-profile revision,
runtime-profile revision, machine bucket, mode and relevant co-residents. A
mode is qualified only for the exact composed configuration that was tested.

Profiles move through `WATCH → LAB → QUALIFIED → RETIRED` (or `REJECTED`). Web
research alone cannot produce `QUALIFIED`. Any artifact, engine, template,
schema, prompt-policy or meaningful tuning change invalidates the affected
witness and sends the profile back to `LAB` until its targeted gates pass.

## Modes (settled shape 2026-08-13; bindings still need witnesses)

A mode is a named workflow binding over the wizard set. It selects one primary
model, optional role specialists, an active subset of selected tools, harness
views, tuning and residency for the same machine. It cannot install or activate
anything outside the wizard set. The wall model is mode-relative: unloading a
large coder returns its allocation to the pool, so a specialist placement that
is illegal in coding mode can be legal in research or helper mode.

Initial mode templates are hypotheses to qualify, not hardcoded universal
bundles:

| Mode | Candidate primary | Optional selected specialists/tools |
|---|---|---|
| research/docs | medium reasoning model | vision/OCR or extraction, web research, reranker |
| planning | large reasoning model | read/search and selected context tools; no mutation by default |
| coding | strongest qualified coder | explicit coding tools and selected search/verification helpers |
| helper | foreground model belongs to the chosen harness | local helper models/tools exposed through installed integrations |

The helper mode is harness-neutral. Pi may use a local model, GPT or Claude via
the user's own API access; Codex or Claude Code may instead be the foreground
harness. Temper manages selected helper services and integrations, while the
harness owns its foreground model, authentication and provider billing.

- **One manifest, per-mode overlays — never N manifests.** Entries carry a
  selected artifact base plus `modes:` runtime-profile bindings (model role,
  engine, placement, context/tuning flags, group, preload, TTL, presence and
  active tool IDs). The generator renders per-mode artifacts. A render fails if
  a binding references an unselected item or an unqualified combination is
  presented as qualified.
- **Roles are the stable interface; modes bind roles to models.**
  Harnesses and extensions speak `rerank`; the mode decides what that
  maps to (jina vs qwen, GPU vs CPU). A tool declares the role it consumes;
  missing required roles make that mode invalid or the tool visibly
  unavailable—never silently substituted.
- **Switching = swapping the active rendered config.** llama-swap
  already watches its config (2s poll), so the mechanism exists today.
  `temper mode <name>` reports what loads/unloads and the warmup cost
  from the render diff. Needs witnessing: reload behavior under
  in-flight requests, and switch latency itself.
- **Parallel harnesses arbitrate through leases, not a daemon.** A lease
  file in state (harness, mode, expiry — renewed while active).
  `temper mode` honors live leases; `--force` stays human. Idle
  detection lives in the harnesses (temper has no watcher — the
  no-daemon rule holds): the harness that notices the coder idle runs
  `temper mode --request helper`, which succeeds only lease-free. Pi
  switches on coder-model switch the same way.
- **Witness cost multiplies by mode and harness.** Each shipped binding needs
  its mechanical soak, real harness protocol tests and role corpus. A template
  may appear as `LAB`, but only qualified bindings are recommended by default.
- **`off` is a mode.** start/stop/mode form one state machine
  (off ⇄ selected workflow modes): every transition is render + kick,
  lease-guarded, recorded. Agents still never run launchctl — they run
  temper, which does, as the stack's own sanctioned tool (the same
  pattern that lets setup.sh do what agents may not), logged and
  condition-stamped.

## Surface

Core transforms (PLAN §10's discipline — the CLI transforms artifacts,
it does not sequence):

- `temper apply` — models.yaml + lock → rendered configs. Fills missing
  lock rows, never moves existing pins.
- `temper update [id]` — re-resolves pins, prints old→new, resets the
  entry's witness to unverified, prints (never runs) the targeted gate.
- `temper check` — budget vs allowance, lock drift, advisory wizard diff.
- `temper mode <name>` — switch the active rendered posture; reports
  what loads/unloads and warmup cost (modes, roles, leases, and the
  off-state: their own section above).
- `temper start` / `stop` / `status` — llama-swap daemon control, i.e.
  the off-mode transitions of the same state machine. `status`
  distinguishes the three levels a launchd service can occupy — job
  loaded / process alive / answering with residency — because
  2026-08-08 witnessed them disagreeing (loaded-but-dead after a clean
  SIGTERM, invisible to `launchctl list`). `stop` prints the wired
  memory it freed — on a 32GB machine that number is *why* the user
  stopped.

Lifecycle:

- `temper init` — the wizard described above: deterministic machine checks,
  model universe, one-by-one tool choices, harness integrations, mode bindings
  and allowances. Writes models.yaml once.
- `temper probe [stage]` — the field kit absorbed (**proposed**: same
  stages, RESULT lines, runtime-profile packet/hash binding,
  tune/deviation/conclude, AGENT.md; `probe` on
  the owner's own box re-witnesses after an update, and on a friend's
  box does what field-kit does today, including keep-or-restore).
- `temper report` — print the current paste-block (probe results, or a
  status snapshot outside a probe).
- `temper uninstall` — the provenance-guided remover.

**Harness integrations are adapters, not providers.** Temper detects supported
harnesses and offers each integration separately. Pi may receive native
extensions/config; Codex and Claude Code receive MCP/plugin configuration; a
generic MCP adapter is possible. Shared helper tools have one core with thin
transport adapters. Installing a harness itself is outside Temper's default
scope, and Temper neither acquires nor validates provider credentials. The
harness owns auth; Temper renders only the selected integration and displays
its local/remote data boundary.

## Artifacts (one writer each)

Labs packets + witnessed measurements produce one reviewed profile. Review
publishes its human evidence to Results and compiles accepted configuration
into the release catalog; the catalog then feeds wizard → models.yaml (user
selection + mode bindings) → lock → generator → configs. Probe results remain
separate local artifacts (`report.md`, `provenance.txt`). Results contains
sanitized conclusions, machine tables and detailed records—not Labs' raw
journal—and is never a runtime dependency. The field kit's current packet is a
signed-by-hash transport into review; it does not skip review, become a Results
recommendation, or become the catalog row itself.

## Labs qualification workflow

Labs keeps decisions reproducible without turning exploratory code into product
code. Model, tool, harness and mode candidates follow the same state machine
and evidence discipline:

1. **Intake / `WATCH`.** Record the question, intended roles/modes, discovery
   sources, official artifact locations, release state and re-check triggers.
2. **Pin / `LAB`.** Resolve exact revisions and hashes; decompose every layer
   that can affect the result. For models this includes weights, the actual
   quantization allocation rather than its advertised bit label,
   tokenizer/template, system-prompt policy and sampling. For tools it includes
   core, transport, schema/description, permissions, backend and harness glue.
3. **Deterministic regressions.** Reproduce cited failures with a known-bad
   fixture, invariant, minimal passing change and false-positive counterexample.
   Security, path/data boundaries, parser/serialization, error propagation and
   silent-loss cases belong here.
4. **Integration witnesses.** Exercise the real engine and streamed harness
   request shape. Run each materially different runtime profile—including
   placement/tuning/co-resident variants—under its actual machine conditions.
5. **Role A/B.** Hold unrelated layers fixed; score first-attempt task success,
   correct selection/arguments, recovery and unnecessary calls before tokens or
   speed. Community templates, prompts, fine-tunes and tool descriptions are
   separate arms wherever possible; unavoidable confounds are declared.
6. **Decision packet.** Emit `QUALIFIED`, `REJECTED`, continued `LAB`, or
   `WATCH`, plus exact scope, raw-evidence index, known failures, rollback,
   invalidation triggers and the smallest candidate profile(s).
7. **Publication and release review.** Publish a laconic Results record with
   current/rejected status, machine scope, detailed sanitized evidence and
   provenance; compile an accepted profile into the catalog. Neither Results
   nor release consumes a moving Labs branch or infers conclusions from raw
   output.

The model-intake implementation begins in `prompts/add-model.md`.
`prompts/update-data.md` now implements the review-to-Results side: it audits
profile identity, provenance, corrections/retractions, conflicting runs,
sanitization and every affected publication surface. A symmetrical
`prompts/add-tool.md` is planned next. The prompts produce dossiers, test plans
and publication candidates, never permission to install or promote. Profile
variants prevent false global conclusions: one model artifact can qualify CPU
and GPU specialist placements or different performance tunings independently,
and one tool can qualify different affordance surfaces for Pi, Codex and
Claude Code.

## Home (**proposed** 2026-08-08): `~/.temper`

Temper's config is machine-witnessed, not portable — a 32GB-witnessed
manifest synced onto a 64GB machine is exactly the lie the witness
system exists to prevent, and people sync `~/.config`. So: one
machine-identity root, `~/.temper` — `models.yaml` (intent), the lock,
`state/` (active mode, leases), provenance, backups. One root also
keeps keep-or-restore and provenance-guided uninstall trivially
auditable (`~/.pi` is precedent next door). Rendered configs stay in
their consumers' homes (`~/.config/llama-swap`, `~/.pi`): temper
renders into other tools' territory, it never relocates it. The real
migration hiding here: the manifest moves out of the repo clone —
today "the install lives in the clone"; under temper the clone is
disposable and `~/.temper` is the machine's identity.

## What ships where (the org, reshaped 2026-08-08 — owner)

The org must carry public value even without the tool. The roster —
each repo independently CI'd, zero-context docs, clean states, high
quality:

- **`temper-sh/temper`** — release: setup + wizard + generator + lock,
  reviewed catalog profiles, acceptance suites, machine-report, README,
  compact applicability/evidence references, harness adapters and the probe.
  It consumes reviewed output; it does not contain exploratory harnesses,
  unresolved candidate research or the full evidence narrative.
- **`temper-sh/field-kit`** — stays the thin public probe repo
  (friend-facing README + curl-able machine-report), becoming a shim
  over `temper probe` at v1 so the "send one file first, then one
  clone" flow survives the rename.
- **`temper-sh/extensions`** — possible common home for harness-specific
  adapters that are independently useful; whether Pi extensions share it or
  become separate projects remains open. Shared tool logic does not fork here:
  each tool has one harness-neutral core and thin Pi/MCP adapters. Every
  installable tool carries qualified profiles describing its tested schema,
  permissions, backend dependencies and per-harness/model deviations.
  Anything universally useful standalone (edit-formats being the first case)
  gets its own project. Pi `packages` remains a candidate distribution path.
- **`temper-sh/edit-formats`** — the pi-edit-formats experiment,
  already split out and nearest to org-ready: the edit-tool-shape
  question measured with success × output tokens, and the
  loud-beats-silent finding (hashline's well-formed-but-silently-wrong
  patches vs string-replace's retryable failures). Note (owner,
  2026-08-08; corrected 2026-08-09): current focus has shifted to
  Claude and other frontier models, where the **scoped** format is the
  measured winner (edit-formats runs 5–6); the local-model
  measurement that motivated it stays paused-and-resumable. Its
  token-economy results are bucket-relative — output tokens cost more
  at 8 tok/s than 13 — so the catalog may eventually cite them per
  row, not as one global verdict. Shipping (owner, 2026-08-09): per
  the rule *what is universally useful standalone goes to its own
  project*, this repo is the **main repo of a standalone, universal
  edit tool** — installable into different harnesses, each harness
  allowed its own deviations only on measured cost saving and
  correctness — and ships the tool itself, not via `extensions`.
  Research continues; whatever format wins, wins (scoped is the
  current measured winner). Name: `temper-editor` proposed;
  `edit-formats` for now. Raw evidence stays in-repo; Results may publish the
  reviewed cross-project conclusion.
- **`temper-sh/results`** — the settled public, human-readable evidence view:
  an eye-level README, an educational guide organized around the owner's
  journey, supported recipes, considered/rejected reasons, an expanding map of
  witnessed machines, and dated proofs with immutable source hashes. Portable
  explanations live in the guide; machine-specific results remain modular. It
  is quality-first and profile-scoped. It publishes reviewed truth; it is not a
  second place to run experiments, decide what ships, select tools or grant
  consent.
- **`temper-sh/labs`** — the decision and data-gathering repo. It keeps model
  and tool intake prompts, watchlists, template/protocol regressions, corpora,
  A/B harnesses, raw result indexes, decision records, failed hypotheses,
  retractions and the active qualification queue. Its workflow is disciplined
  even when its experiment code is disposable: preregister metrics and stop
  rules, pin every layer, capture conditions, retain raw evidence, and emit a
  reviewable profile-candidate packet. Whether the repo is public remains an
  owner decision; its product boundary does not depend on visibility.

Release never reads raw Labs or Results state at runtime. Promotion is an
explicit review with two outputs—a Results publication and a catalog
copy/compile—not a live cross-repo dependency. Public cross-linking policy for
Labs remains an owner decision; Results and the shipped catalog identify
evidence revisions well enough to audit a row.

## Quality bars

Release-bar: shellcheck-clean bash 3.2 (scripts), hermetic offline suite,
second-run-clean, --dry-run purity, no launchctl/sudo from tests. The Go
TUI/CLI (if §10's port proceeds) is diffed against the bash generator as
oracle — byte-identical configs before any cutover.

Labs-bar for a promotable packet: exact revisions and hashes; artifact-layer
decomposition; deterministic regression fixtures; real harness/API tests;
role-corpus first-attempt results; machine conditions and raw artifacts;
declared thresholds/stop conditions; license and data-boundary review; known
confounds; and a minimal proposed profile. The model prompt in
`prompts/add-model.md` is the first concrete intake template. Tool intake must
reach the same standard before tool profiles enter the release catalog.

## Non-goals

- No model-quality rankings without a task-success corpus behind them.
- No weight mirroring or redistribution; cache pre-seeding stays the
  documented path.
- No daemon beyond llama-swap; no background updaters; no phoning home.
  (`temper start/stop` controls llama-swap's launchd job; temper itself
  stays a CLI, and the idle-watcher lives in harnesses, never here.)
- No Linux/Windows in v1 (the measurements are Apple-Silicon-specific;
  the wall model doubly so).

## Milestones (proposed)

- **M0 — generator extraction** (§10 sequence step 1; unblocks wizard,
  probe, and the Go-oracle diff at once).
- **M1 — lock + apply/update/check** on the extracted generator.
- **M2 — catalog schema + Labs promotion packet** for model, engine, tool,
  harness and mode profiles; retain the model-intake and Results data-update
  workflows and define engine/tool intake.
- **M3 — wizard TUI** over a curated model universe, individually opt-in tools,
  harness integrations and mode bindings.
- **M4 — probe absorption** (`temper probe` = field-kit stages) plus
  mode/harness qualification surfaces.
- **M5 — the split**: Labs/release extraction, Results publication wired into
  review, and the catalog seeded only with reviewed qualified rows.

## Open questions (owner)

1. Distribution: brew formula vs curl-installer vs release-asset binary
   (and §10's Go-scope decision folds in here).
2. Which of research/docs, planning, coding and helper have enough complete
   model + tool + harness evidence to ship as qualified v1 mode profiles?
3. Catalog contribution flow: how a friend's probe report becomes a row —
   hand-curated by the owner (current stance) or a structured submission?
4. Is remote-provider integration strictly render-only (current direction),
   with credential and foreground-model ownership left to each harness?
5. Versioning of witnessed rows when engines move: a row's witness pins
   engine versions — does `update` invalidate the row or fork it?
6. llama-swap mechanics the modes design leans on: role aliases
   (checkable), and config-reload behavior under in-flight requests
   (witnessable — a mode-switch-under-load probe measurement).
7. Lease semantics: is an advisory state file with expiry enough for
   cooperating harnesses (leaning yes — hostile tools are not the
   threat model), and is `--force` human-only (leaning yes)?
8. Do mode witnesses become a first-class probe surface —
   `temper probe --mode <name>` soaking the non-default posture?
9. Pi `packages` as an adapter distribution channel, plugin packaging for
   Codex/Claude Code, and the standalone CI contract for a shared tool core.
10. Catalog representation: separate typed profile documents or one normalized
    graph whose rows can express model, engine, tool, harness and mode evidence
    without making user consent implicit.
