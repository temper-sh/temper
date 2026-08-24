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
locally 2026-08-13; `temper-sh/field-kit` already exists and keeps owning
the probes, running them over the base Temper installs (below, 2026-08-14).

The manifest file is **`manifest.yaml`** with **`manifest.lock.yaml`**
beside it (decided 2026-08-14: it carries the whole wizard selection —
tools, harness integrations and mode bindings, not just models — so
`models.yaml` misnamed it; the legacy repo's `models.yaml` keeps its name).

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
2. **The friend with a different Mac**: runs the field kit, sends back
   one text block, gets to keep or fully remove
   the result. Their machine's witnessed combination can become a catalog
   row.
3. **The AI agent driving either of the above**: first-class. Stable
   RESULT lines, machine-parseable outcomes, an interpretive runbook
   (AGENT.md's evidence model), consent gates that stay human, and a
   `conclude` channel so its analysis lands in the artifact, not a chat
   log.

## Principles (inherited, non-negotiable)

- **Measured beats plausible.** A qualification-catalog row ships only after
  Labs produces a reviewable qualification packet and a witnessed run
  (machine, SHAs, date, conditions, corpus and numbers). A mechanical probe
  can rule a combination *out*, never establish model or tool quality by
  itself. Software-supply records may describe resolvable packages, but only
  cited tested-version evidence may call a version tested.
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
model browser.

**Reordered to modes-first, 2026-08-19 (owner).** It now proceeds:

1. detect hardware and allowances, then **choose the modes** to set up;
2. **one screen per chosen mode** — its models, its tools, its harnesses,
   and for each model whether it stays loaded;
3. preview downloads, residency, mode transitions and external data paths;
4. write the manifest once, then render and probe it.

The previous order collected models, then tools, then harnesses, then asked
the user to sort those choices into "workflow-mode templates built only from
steps 2–4" — which asked people to re-sort decisions they had already made
into worlds they had not yet seen. Modes-first asks in the order the work is
actually thought about: *which ways of working do I want*, then *furnish each
one*. It also makes the machine's limits the first thing on screen rather than
a disappointment discovered after picking models that do not fit.

The old numbering survives where other documents cite it: former steps 2–4
(models, tools, harnesses) are now the contents of a mode screen, and former
step 5 no longer exists as a separate stage.

### Screen 1 — the machine, then the modes

```
  Apple M5 · 32 GiB · 144 GiB free
  One large model resident at a time on this machine.

  Which ways of working should be set up?

  [x] local     a local coder does the work; Pi talks to it
  [x] utility   your harness brings its own model — Claude Code, Codex, or
                Pi on a provider you already pay for. Local tools and small
                specialists stay available

  off is always there.
```

**Which modes appear is catalog-derived, not a bucket→mode table:** a mode is
offered when the catalog can furnish it on this machine. A mode that cannot be
furnished is shown *disabled with its reason* rather than hidden — the reason
is the most useful thing on the screen for that user:

```
  Apple M2 · 16 GiB · 210 GiB free

  [ ] local     unavailable — no coder layout qualified at 16 GiB
  [x] utility   …
```

### Screen 2 — one per chosen mode

Each mode screen carries that mode's members, tools and harnesses. The screens
differ in shape, which is the point: the utility screen never asks about
coders, and the local screen never offers GPU placement for a specialist,
because a resident coder owns the GPU.

```
  local — a local coder does the work

  Coders — recommended for this machine; install any subset, one runs at a time
  [ ] Qwen3.8 · llama.cpp · plain Q4 · 128k
      throughput/context: faster controlled decode; largest qualified window
  [ ] Qwen3.8 · llama.cpp · Dynamic 3.0 XL · 100k
      quality-first: slightly lower perplexity and fuller answers; slower decode
      Pi starts on: choose after selecting one or more coders

  Tools
  [x] project-search   needs a reranker → adds Qwen3-Reranker-0.6B,
                       CPU here because a coder holds the GPU
                       local only, nothing leaves the machine

  Harnesses
  [x] Pi
                                       this mode downloads   32.3 GiB
```

```
  utility — your harness brings the main model

  No local coder resident: ~22 GiB free, so several specialists can be
  resident here at once.

  Local helper models                            keep loaded?
  [x] rerank 0.6B     powers project-search      ( ) yes   (•) on demand
                      loaded: holds 1.6 GiB · on demand: 2.4 s first use
  [ ] extract 3B      structured extraction      ( ) yes   (•) on demand
  [ ] vision 4B       screenshots and diagrams      not yet qualified

  Harnesses
  [x] Pi   [x] Claude Code   [x] Codex
           prompts go to your existing accounts; Temper never sees credentials

                                       resident when idle    0.0 GiB
                                       downloads             6.9 GiB
```

Rules the screens must follow:

- **"Pi starts on" is how a mode names its preferred resident.** It is a radio
  beside the coders rather than a separate concept, and it is the manifest's
  per-member default flag. At most one per mode.
- **Keep-loaded is asked per model, per mode**, and it is what places the
  member in `resident:` or `on_demand:`. The same reranker is on demand beside
  a coder and may be resident when nothing else is — which is why the question
  belongs on the mode screen and not on the model.
- **Default to on demand, and show both halves of the trade in the same
  line.** For the reranker the numbers are measured: 1.64 GiB held
  continuously against a 2.4 s cold load inside `project_search`'s 180 s
  budget. Showing both lets a different workload decide differently instead of
  trusting an invisible default.
- **"Resident when idle" is a mode's headline number**, more than its
  download: it is what the machine pays while the user is doing nothing. In
  utility mode it can honestly be 0.0 GiB, which local mode can never say.
- **Consent is per tool; exposure is per mode.** A tool's first appearance is
  the full consent question — backend, data boundary, the role-models it drags
  in. Later appearances are a plain checkbox reading "also here?". A two-mode
  setup must not re-ask everything, or people stop reading the consequences.
- **Harness detection is global; enabling is per mode.** Whether Claude Code
  exists on the box is a screen-1 fact; whether it is wired in *this* mode is
  a mode fact, matching the per-mode rendering of the adapter configs.
- **The download bill is a union, not a sum of screens.** Choosing
  `project-search` in two modes adds its reranker once. Per-screen totals say
  "this mode"; the preview shows the real number.

### The model section of a mode screen selects layouts, not models

A model step does not select a *model*. It selects a **layout**: the artifact,
engine and tuning unit that actually has a context window, a speed, a memory
shape and a harness derivation. Plain-Q4 Qwen3.8 at 128k and Dynamic 3.0 XL at
100k are therefore different products even though they share a model family;
a user choosing between them is choosing a way of working, not merely a model
name.

### Recommendation is a set, not a winner

**Settled 2026-08-20 (owner): Temper may recommend several layouts for the
same machine, role and mode.** A valid tradeoff is not demoted to “alternative”
merely because another valid tradeoff exists. The witnessed M5/32 GiB example
is Qwen3.8 plain Q4 at 128k (more context and faster controlled decode) beside
Dynamic 3.0 XL at 100k (the modest quality-first profile). Both passed the
practical task gate; neither dominates every axis, so both are recommended.

Three catalog concepts stay separate:

- **`QUALIFIED` is evidence validity:** the exact artifact/runtime/machine
  witness cleared its gates.
- **Recommended is applicability:** among qualified rows, this layout is a
  sensible choice for this machine, mode and job. Zero, one or many layouts may
  be recommended. Recommendation does not imply a total order or a default.
- **Selected/preferred is user intent:** only an explicit checkbox puts a
  layout in `manifest.yaml`, and only the user's “Pi starts on” radio makes one
  preferred. A recommendation never checks either control.

Each recommended layout carries an evidence-backed **performance profile** in
its model-runtime qualification record—not a new profile kind and not a scalar
score. The comparison view covers first-attempt task success and known
regressions first, then task wall time/tool use, raw decode and prefill,
qualified context threshold, resident/full-slot memory, cache/replay behavior,
and the exact conditions behind every number. Unknown axes say unmeasured.
The wizard explains the material differences within a comparable group and
lets the user keep several layouts installed; it never collapses them into an
opaque “best model” ranking.

**Layouts are described by what they are for.** The specs are the evidence
behind the sentence, not the sentence. Three rules on the numbers that do
appear, each from a legacy measurement that contradicts the obvious phrasing:

- **One definition of context, applied to every row.** Raw window and
  usable-after-reserve differ by the compaction reserve, and mixing them
  across rows makes the comparison meaningless. Raw window is stable; usable
  moves whenever the reserve changes.
- **Task-level speed, never token-level.** Decode alone separates these two
  layouts by ~2.5× and prefill by ~1.6×, but per finished task the measured
  spread is 1.2–1.6× (legacy FINDINGS #27). A wizard advertising "2× faster"
  teaches the mistake the quality bar exists to prevent: the fast layout
  finished 0 of 4 attempts at the hard task class, the slow one 5 of 5.
- **Context is a threshold, not a dial.** "More/less context" reads as a
  preference. The measurement is a cliff — debug-class agentic work peaks near
  19k, so 16,384 is not *less* context for that work, it is *not enough*, at
  any speed. Say which task classes a layout finishes; the window is
  supporting detail.

**Selection means installed, not resident.** A ticked box is *downloaded and
configured*; residency is the separate keep-loaded question, and in `local`
only one coder runs at a time whatever is ticked. This is the layout/mode
split the manifest models (`docs/design/manifest-schema.md`: layouts say what
a thing *is*, modes say what is *live*). The screen must state it in-line,
because two ticked boxes read as "both running" and on a 32 GiB machine that
is the one thing that cannot happen.

What multi-select does not fix: switching is *request*-driven, so a request
naming the other layout evicts the running one and takes an in-flight turn
with it. Installing both is what makes that reachable, which is why the mode
machinery (M4) owns leases and `--request` rather than leaving arbitration to
convention.

**Each mode screen shows a running download total** for the current selection,
before consent — not per-row sizes. The rows stay about purpose, and a total
that moves as boxes are ticked demonstrates the sum better than a column of
numbers would. The preview still shows the full bill, unioned across modes;
this is the same number surfaced at the moment of choosing. Four traps, all
measured in the legacy stack:

- **Per file, not per repo.** Quant publishers ship many quants in one HF
  repo: `unsloth/Qwen3.8-27B-GGUF` held 95 GiB across three quants, of which
  the used file is 15.9 GiB. A repo-size number is six times wrong.
- **Sidecars count.** The MLX layout is ~15 GiB target plus a ~0.25 GiB MTP
  drafter — the whole artifact set the entry pins.
- **Layouts of the same model share nothing.** GGUF and safetensors are
  different artifact families; ticking both Qwen3.8 rows is ~31 GiB, not ~16.
  The user cannot be expected to know this, so the total must show it.
- **Helpers are on the bill.** The reranker arrives with `project_search`
  whether or not the user thought about it.

The size is **declared in the catalog profile and verified at download time**.
The wizard runs before anything is fetched so it cannot measure, and a live
upstream query would put the network on a screen that must work offline —
declared-then-verified is the same discipline the lock applies to hashes.
This is also what the disk allowance is checked against, at the last moment
the user can still change their mind.

### Utility mode is where shelved specialists become possible

The wall is mode-relative, and utility mode is the extreme case: with no coder
resident the GPU budget is free, so several specialists can be resident at
once. Two models the legacy stack shelved were shelved for co-residency
reasons alone, not on quality — extraction (retired 2026-08-08 because the
32 GiB posture is coder-only on the GPU) and vision (parked "pending a
placement decision"). Utility mode *is* that placement decision, and it is the
only place either has a route on a 32 GiB machine.

**`utility`, not `helper`** (renamed 2026-08-19): "helper model" is a *role*
that appears in any mode — the reranker is in both, and the planned 1–2B
compactor must run in local mode beside an idle coder, because the 27B's own
compaction cannot fit at the moment it fires. Naming the mode `helper` would
collide with the role.

Several large models may be installed and available. By default only one large
local model is resident or serving at a time; a mode switch unloads before it
loads another unless a machine-specific witnessed profile explicitly permits
co-residency. Small specialists remain on demand or CPU-placed according to the
mode's witnessed resource profile.

**Software installation has a portable strategy and a target adapter.** The
software-supply catalog calls `system-package`, `python-environment`,
`release-artifact`, or `source-revision` an installation **method**. A concrete
**adapter** executes that method on a target: `homebrew` is the current macOS
system-package adapter, while a future target may explicitly bind the same
method to `apt`, `dnf`, `pacman`, `winget`, or another reviewed implementation.
The method/adapter boundary is a keyed adapter family, not OS conditionals in
the installer. Every adapter package supplies the same narrow,
provider-neutral contracts for resolver reads, state inspection, installation
and removal effects, and reconciliation; planning remains pure and vendor
command/output types stop at the adapter edge.

Target selection may use only a catalog-declared adapter binding, and the exact
method, adapter, target, versions, and closure are written to the software lock
and installation receipt. An adapter definition is the one home for its
method, supported targets, effect class, and capabilities; an adapter-native
package recipe references that definition and owns package names and version
rules. Each catalog snapshot names at most one canonical adapter for a method/target;
alternatives are explicit variants rather than environment-driven guesses.
Choosing the declared target adapter inside the same method is deterministic
target resolution; changing methods is explicit and never a fallback.

**Catalog curation starts from the concrete package, not a preferred package
manager (owner, 2026-08-20).** This is the Temper-wide rule, including but not
limited to the Field Kit base. For a Temper-managed application, release review
first considers a verified isolated upstream artifact; Python applications use
an exact Temper-owned `uv` environment when their dependency closure is part of
the qualification; `system-package` is reserved for bootstrap tools, genuine
system-wide dependencies, packages available only there, or a distribution
shown to be materially more maintainable. `source-revision` is the last resort.
This order chooses which reviewed recipes the catalog offers; it is never an
automatic fallback chain at resolution or installation time.

On the current macOS target, Homebrew is an acceptable owner for the shared
bootstrap tools `uv` and `hf`. It does not own the Python applications that uv
installs: uv selects and materializes each exact interpreter and package closure
inside a Temper-owned environment. The Hugging Face CLI is likewise a tool, not
model identity. Temper may use only revision-pinned `hf` operations; rendered
llama-server commands still use an exact local `-m` path with `--offline` and
never the moving llama.cpp `-hf` shortcut. Homebrew, uv, hf, and every package
they install remain visible catalog/lock/receipt facts rather than ambient
commands discovered from `PATH`.

The initial managed inventory is deliberately finite. `uv` and `hf` are the
shared Homebrew-managed bootstrap tools. `llama-swap` and `llama.cpp` form the
Field Kit serving base. The 2026-08-24 static and bounded runtime comparison
selected isolated `release-artifact` installation through the compiled
`upstream-release` adapter; recipe publication still waits for its real
scratch install/check/remove/second-run gate. Supported,
non-default `rapid-mlx` and `mlx-dspark` use explicit
`python-environment`/`uv` recipes so their Python and MLX closures are isolated
and locked. Pi is not in this inventory: it is a user-managed harness whose
selected integration authorizes configuration rendering, not installation. v1
qualifies only Apple Silicon, but this schema and workflow do not make Homebrew
or macOS the domain abstraction.

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
- an **engine profile** references the exact tested software-supply identity
  and declares API/capability surface, process isolation, service contract and
  known engine-wide failures; the install recipe and resolved dependency
  closure have one home in the software supply catalog and software lock;
- a **model runtime profile** references an artifact and engine profile and pins
  placement, context/KV settings, sampling/thinking, batching, speculation,
  residency, preload and TTL for a machine bucket and mode;
- a **tool profile** pins the tool core, transport, schema/description,
  backend role, permissions and harness/model affordance deviations;
- a **mode profile** composes qualified artifact/runtime and tool profiles for
  a job, and may override runtime configuration per selected model;
- an **activity profile** such as inspect/change/verify/review narrows the
  active tools inside a mode and never widens them.

**Harness client settings are profile derivations (2026-08-17).** A harness
carries client-side settings that are *functions of the selected model's
window* — the witnessed case is Pi's auto-compaction
(`reserveTokens`/`keepRecentTokens`): its frontier-sized defaults zero the
compaction threshold against a 16k local window, which turned the context
ceiling into stranded sessions (legacy FINDINGS #25). Such settings belong
to the profile that selects the model, expressed as derivations (the legacy
stack derives `reserve = window/8`, `keep = (window − reserve)/3`), never
constants. Applying or switching a mode re-materializes them for the tightest
selected window — and because a client's settings surface may be global (Pi's
is, per-project overrides aside), a mode that pairs the same harness with a
frontier model re-derives frontier-sized values instead of inheriting a local
mode's tight ones.

Both formulas were corrected on 2026-08-19 from the values first recorded
here, and each correction is a witnessed failure worth keeping: the reserve
carried a `maxTokens` term until the KV budget clamp made client-side
generation insurance unnecessary, and the keep was `/2` until a
post-compaction floor landed *above* the trigger and put the session on a
fire → compact → still-over → fire trajectory. Anything that re-derives these
must treat both fields together — a profile that sizes the reserve while
inheriting a global keep reproduces exactly that thrash.

**The derivation follows the harness's foreground model, not the resident set
(2026-08-19).** In `local` the foreground is our coder — the narrowest
resident coder when several are installed together. In `utility` the
foreground belongs to the harness, so the harness's own defaults stand, and
local members never feed the derivation at all.

The distinction is not pedantic; deriving from residents is actively wrong in
utility mode. Tool-backends are already exempt because no harness chats with
them — a reranker's 4k context has never had anything to do with compaction.
But utility mode may also offer a *small local chat model*, a 6–8k helper the
user can select in Pi. Electing the narrowest resident would then write a pair
sized for 8,192 into a global slot that Pi applies to the frontier model doing
the actual work — reserve ~1,024, keep ~2,389, against a 200K window. Temper
cannot even see that window to know better, because the harness owns it. So
the rule is the foreground model, and where the foreground is not ours, the
answer is "leave it alone".

This is also the sharpest form of the case for per-model settings: with one
global slot, **a frontier model and an 8k local helper cannot coexist in one
harness**, whatever value is chosen. Election is safe only while every chat
model in the mode is a similar-sized coder.

**Per-model resolution is the target, election is the fallback (2026-08-19).**
Where a client can express settings per model, the renderer emits one set per
resident layout and no compromise is needed; where it exposes one global slot,
the renderer elects the narrowest resident window, because a shared setting
must be survivable by the smallest window in the mode. Upstream
`earendil-works/pi#8133` requests exactly the per-model form and has been
reopened past that repo's auto-close gate — a maintainer signal, not a
schedule. **The manifest is identical either way**: this is a rendering
concern, a property of the target client's capability, and it must never reach
the schema. `docs/design/manifest-schema.md` carries the three-way resolution
including the local-patch middle option.

Runtime profiles are deliberately plural for one artifact. On the same 32GB
machine, a reranker may use `-ngl 0` and short TTL in `local` so the large
coder owns the GPU, then use GPU placement and a longer TTL in `utility` after
that coder unloads. The lock deduplicates the artifact download but pins each
runtime-profile revision and rendered-config hash separately.

Where the plurality lives was settled 2026-08-19: **placement is a mode fact,
identity is a layout fact.** The reranker's `-ngl` differs by mode and stays a
member flag, because offload does not change what the model produces. A
different context cap, KV precision, MTP or thinking mode *does* change what it
produces, so those make a second layout rather than a per-mode override — which
is why the per-mode `tuning:` block was dropped. `docs/design/manifest-schema.md`
carries the line and the test cases.

The current field-kit bridge serializes an exploratory runtime witness as a
`field-kit-runtime-profile/v1` packet: exact packet, evidence and (when the
normal stack can execute it) manifest hashes are verified, and the packet
identity is stamped into runtime measurements only after the selected manifest
is active; preflight labels it pending. A tune invalidates the identity.
An `external-lab` packet is inspectable but cannot enter the generic install
path. This is the Labs-to-probe handoff format, not a premature decision about
the qualification catalog's normalized schema and never a consent token.

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

A mode is a **world**: which layouts are served and resident, which tools are
exposed, which harnesses are wired, and what the harness settings derive to.
A mode switch rebuilds that world. It cannot install or activate anything
outside the wizard set. The wall model is mode-relative: unloading a large
coder returns its allocation to the pool, so a specialist placement that is
illegal beside a resident coder is legal without one.

**Two modes, plus `off` (owner, 2026-08-19 — replacing the four-template
table).** The axis that makes a mode is *who owns the foreground model*:

| Mode | Foreground model | Resident set |
|---|---|---|
| `local` | ours — a coder layout the user installed | one coder (more where the wall allows), specialists on demand |
| `utility` | the harness's own: Claude Code, Codex, or Pi on a provider | no coder; specialists may be resident and may use the GPU |
| `off` | none | nothing |

The earlier table listed research/docs, planning, coding and helper as four
modes. Three of those differed only in *which tools were active* over the same
resident coder — that is the SPEC's own **activity profile**, "narrows the
active tools inside a mode and never widens them", not a separate world. Only
helper changed who owns the foreground model, which is what a mode is. Two
consequences of collapsing them, both good: the witness cost stops multiplying
by mode (there are two resource configurations to soak, not four, and tool
narrowing needs a permissions test rather than a resource witness), and the
wall model has two answers instead of a per-mode table.

Modes are deliberately not a fixed list. A machine that can host several large
models may want more than one `local` world; the schema does not care, and the
wizard offers what the catalog can furnish on that machine.

Utility mode is harness-neutral. Pi may use GPT or Claude via the user's own
API access; Codex or Claude Code may instead be the foreground harness. Temper
manages selected helper services and integrations, while the harness owns its
foreground model, authentication and provider billing — which is also why the
compaction derivation leaves that harness's settings alone in this mode.

- **One manifest, per-mode layouts — never N manifests.** (Reshaped
  2026-08-14: mode-first, not per-entry overlays — a `modes:` section
  where each mode lists its members and their bindings in one place, so
  the layout reads at the mode.) A mode's bindings carry model role,
  engine, placement, context/tuning flags, group, preload, TTL, presence
  and active tool IDs. The generator renders per-mode artifacts. A render fails if
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

- `temper apply` — manifest.yaml + lock + previously hash-verified immutable
  artifact sets → rendered configs. The bootstrap native slice reads a
  complete lock and refuses gaps, malformed receipts, and absent selected
  sets even during dry-run.
- `temper resolve` — fills missing model rows from authoritative upstream
  metadata without moving an existing pin or downloading model weights.
- `temper fetch <layout>` — materializes exactly one selected, pinned layout
  as an immutable verified artifact set; there is no implicit fetch-all.
- `temper update [id]` — re-resolves pins, prints old→new, reports whether the
  new pin leaves the active catalog's tested set, and prints (never runs) the
  targeted gate. The M1 implementation moves existing rows through one
  concurrency-safe lock commit and never downloads weights; tested-set reporting joins it with
  the M2 qualification catalog. There is no locally stored verified/unverified
  state.
- `temper check` — read-only lock and local-artifact audit plus a labeled
  resident wall-model prediction from live machine allowances and admitted
  model sizes; `--verify` explicitly streams selected files against lock
  hashes. Tested-catalog membership, served-model drift, and advisory wizard
  diff land in later slices.
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

- the software-supply lifecycle (**sequenced first 2026-08-20**) — resolve a
  provider-native policy through the target's declared adapter into an exact
  `software.lock.yaml`, or accept an exact lock generated for an explicit
  experiment, then install/check/remove that closure as one named installation
  and record observed state in its receipt. Experiment and catalog provenance
  may coexist; installation never needs the active catalog. "Latest" and "minimum
  tested" are inputs to explicit resolution, never floating installed state.
  Every effect runs through the adapter family; changing installation method,
  such as `system-package`/Homebrew → `python-environment`/`uv`, is explicit,
  and shared package-manager state is touched only through a root-wide claim,
  acquisition, and reconciliation contract. Experiment locks may require exact
  verified base-lock receipts and keep isolated software below their own named
  installation directories;
- `temper init` — the wizard described above: deterministic machine checks,
  model universe, one-by-one tool choices, harness integrations, mode bindings
  and allowances. Writes manifest.yaml once.
- the probe base (**decided 2026-08-14; moved to M2 immediately after the
  supply catalog on 2026-08-20**): probes belong to the field kit; there is no
  `temper probe`. Temper installs the exact locked basic requirements and
  exposes the reversible base the field kit consumes — canonical machine
  facts, provenance, llama-swap and basic dependencies, isolated profile
  rendering, scoped service lifecycle, artifact verification, and removal of
  only what its receipt and the root-wide claim state permit. A stable base and
  multiple experiment locks/receipts may coexist below one explicit root.
  Stages, RESULT lines,
  tune/deviation/conclude, AGENT.md and keep-or-restore stay field-kit's;
  packet identity binds the Temper binary, the ordered base/experiment
  installation-lock-receipt set, manifest lock, generation, and machine facts.
  Re-witnessing after an update is a
  field-kit run against this base.
- `temper report` — print the current status-snapshot paste-block (probe
  reports are field-kit artifacts — 2026-08-14).
- `temper uninstall` — the provenance-guided remover.

**Harness integrations are adapters, not providers.** Temper detects supported
harnesses and offers each integration separately. Pi may receive native
extensions/config; Codex and Claude Code receive MCP/plugin configuration; a
generic MCP adapter is possible. Shared helper tools have one core with thin
transport adapters. Harness executables are external user-managed tools; Pi is
the first concrete case. Selecting a harness in the manifest permits Temper to
render and check that integration only. It never permits Temper to install,
upgrade, remove, or choose the harness, its language runtime, or its package
manager. Temper neither acquires nor validates provider credentials. The
harness owns its executable, auth, foreground model, and provider billing;
Temper renders only the selected integration and displays its local/remote data
boundary.

## Artifacts (one writer each)

Software and configuration have separate fact chains:

- release-reviewed software supply records define method, target-adapter
  binding, provider-native version policy, constraints, recipes, and
  tested-version evidence → explicit resolution writes the exact desired
  `software.lock.yaml` → the selected adapter installs, inspects, and writes the
  actual installation receipt;
- Labs packets + witnessed measurements produce reviewed qualification
  profiles → release review publishes human evidence to Results and compiles
  accepted configuration into the qualification catalog → the wizard writes
  `manifest.yaml` (user selection + mode bindings) → `manifest.lock.yaml` →
  generator → configs.

The software lock does not claim installation, the receipt does not select an
update policy, and the manifest lock does not duplicate either. Probe results
remain separate local artifacts (`report.md`, `provenance.txt`). Results
contains sanitized conclusions, machine tables and detailed records—not Labs'
raw journal—and is never a runtime dependency. The field kit's current packet
is a signed-by-hash transport into review; it does not skip review, become a
Results recommendation, or become a qualification-catalog row itself.

Software-supply catalog snapshots are signed and published independently of
the Temper binary. The binary owns supported schemas, adapter protocols,
compiled adapters, and the signing trust root; a software lock owns the exact
snapshot schema, monotonic sequence, and SHA-256 used for resolution. An
explicit catalog update may atomically move the active snapshot only. It never
rewrites a lock, resolves or installs software, or changes an installation
receipt, and there is no background updater.

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
machine-identity root, `~/.temper` — `manifest.yaml` (intent), the lock,
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
  compact applicability/evidence references, harness adapters and the probe
  base.
  It consumes reviewed output; it does not contain exploratory harnesses,
  unresolved candidate research or the full evidence narrative.
- **`temper-sh/field-kit`** — stays the thin public probe repo
  (friend-facing README + curl-able machine-report) and **keeps owning the
  probes** (2026-08-14, replacing the earlier shim-over-`temper probe`
  idea): it orchestrates its stages, packets, consent gates and
  keep-or-restore over the base Temper installs, and the "send one file
  first, then one clone" flow survives unchanged.
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
Labs remains an owner decision; Results and the published catalog identify
evidence revisions well enough to audit a row.

## Quality bars

Release-bar: shellcheck-clean bash 3.2 (scripts), hermetic offline suite,
second-run-clean, --dry-run purity, no launchctl/sudo from tests. The Go
TUI/CLI (decided 2026-08-14: the wizard is TUI-heavy with certainty, so
per the split-brain rule the whole CLI is Go) uses native table tests and
config goldens. The extracted Bash renderer is optional comparison evidence
at cutover, not a compatibility surface or byte-identity requirement; every
intentional difference is reviewed.

Labs-bar for a promotable packet: exact revisions and hashes; artifact-layer
decomposition; deterministic regression fixtures; real harness/API tests;
role-corpus first-attempt results; machine conditions and raw artifacts;
declared thresholds/stop conditions; license and data-boundary review; known
confounds; and a minimal proposed profile. The model prompt in
`prompts/add-model.md` is the first concrete intake template. Tool intake must
reach the same standard before tool profiles enter the qualification catalog.

## Non-goals

- No model-quality rankings without a task-success corpus behind them.
- No weight mirroring or redistribution; cache pre-seeding stays the
  documented path.
- No daemon beyond llama-swap; no background updaters; no phoning home.
  (`temper start/stop` controls llama-swap's launchd job; temper itself
  stays a CLI, and the idle-watcher lives in harnesses, never here.)
- No Linux/Windows in v1 (the measurements are Apple-Silicon-specific;
  the wall model doubly so). This limits released target adapters and
  qualification evidence, not the catalog/lock vocabulary or installer
  boundary; future platforms add adapters rather than a second workflow.

## Milestones (proposed)

- **M0 — legacy generator extraction**, completed as reference work; its
  planned product landing was retired 2026-08-19 when the owner chose the
  native manifest directly.
- **M1 — native manifest + lock + resolve/fetch/apply/update/check**, complete
  2026-08-20; rendering, pin management, exact artifact materialization,
  receipt/full-hash admission, and the resident wall-model prediction execute
  in Go with no Bash runtime dependency.
- **M2 — software supply catalog + Field Kit installed base, then the broader
  qualification catalog**. First model rolling/guarded/constrained package
  policy and exact software locking; next install/check/remove the receipted
  reversible base Field Kit consumes; then add model, engine, tool, harness,
  activity and mode qualification profiles plus the Labs promotion packet.
- **M3 — wizard TUI** over a curated model universe, individually opt-in tools,
  harness integrations and mode bindings.
- **M4 — production mode state machine + harness qualification** over the
  already installed Field Kit base: active mode, service reconciliation,
  leases, harness protocol, and witnessed bindings.
- **M5 — the split**: Labs/release extraction, Results publication wired into
  review, and the catalog seeded only with reviewed qualified rows.

## Open questions (owner)

1. Final public distribution: brew formula vs curl-installer vs release-asset
   binary. A directly supplied checksummed pre-release binary is sufficient
   for M2 Field Kit work and does not settle this choice.
   (The Go-*scope* half that used to fold in here was settled 2026-08-14 —
   the whole CLI is Go; only distribution remains.)
2. Do `local` and `utility` each have enough complete model + tool + harness
   evidence to ship as qualified v1 modes? (Reframed 2026-08-19: the four
   candidates collapsed to two, so this is now two questions rather than
   four, and the tool-narrowing that used to distinguish planning from coding
   needs a permissions test rather than a resource witness.)
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
8. Do mode witnesses become a first-class probe surface — a field-kit
   experiment package soaking a non-default mode posture? (Reframed
   2026-08-14: probes are field-kit's, so this is a field-kit/Labs
   question; Temper's part is only that the base can render and serve the
   requested posture in isolation.)
9. Pi `packages` as an adapter distribution channel, plugin packaging for
   Codex/Claude Code, and the standalone CI contract for a shared tool core.
10. Catalog representation: separate typed profile documents or one normalized
    graph whose rows can express model, engine, tool, harness and mode evidence
    without making user consent implicit.
