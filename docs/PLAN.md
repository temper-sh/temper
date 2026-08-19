# Temper — execution plan

Created 2026-08-14. This is the product repository's working plan: what gets
built, in what order, where each piece is developed, and what gates it.
Sources: [SPEC.md](SPEC.md) (the adopted product spec, milestones M0–M5),
the legacy repo's PLAN §10 (the reshape sequence — superseded by this file
for execution detail; a pointer there says so), and the Temper Labs
scaffold's boundaries and product-handoff contract (`../labs`).

Status legend used below: an item is **design**, **build**, **measure**
(a Labs experiment), or **decide** (owner). Nothing in this file authorizes
a heavy run, a download, or a change to the live machine.

## 0. Where work happens and how it arrives

The org has four working repos plus the legacy one:

| Repo | Role | What it sends this repo |
|---|---|---|
| `../labs` | decides and gathers evidence | reviewed profile packets, accepted product handoffs |
| `../results` | explains reviewed evidence to people | nothing at runtime; shared evidence identifiers |
| `../field-kit` | frozen portable tests on consenting machines | witness reports, via Labs review |
| `../local-ai-setup` (legacy) | running reference implementation + evidence history | the extracted generator (M0), the byte-diff oracle, behavior reference |
| **this repo** | ships reviewed configuration + the minimum probe environment | — |

Work enters this repo through exactly two doors:

1. **Product handoffs** from Labs: a `PROPOSED → ACCEPTED` record in
   `../labs/product-handoffs/` with the product bar filled in. The
   destination (here) owns the extraction or rewrite; prototypes are
   rewritten to this repo's bar, never copied because they worked once.
2. **Product engineering planned here**: schemas, CLI verbs, wizard, CI —
   work that was never an experiment.

**Development locus (stance D12 — for owner confirmation).** The legacy
PLAN §10 sequence predates this repo and placed its first steps "in this
repo [legacy] before extraction." Restated now that the labs and temper
scaffolds both exist:

- **M0, the generator extraction, still happens in the legacy repo.** It is
  a refactor of legacy's own `steps/40-configs.sh` against legacy's own
  84-check offline suite, and the live machine runs that code today. The
  extracted executable and its oracle harness then land here via a handoff.
- **M1 onward is developed here**, against fixtures plus the M0 executable.
  The legacy manifest is read-only reference material.
- **The live machine stays on legacy `setup.sh` until the M5 cutover gate.**
  Nothing in this repo touches the running service before then; the live box
  becomes Temper's first witness machine *at* cutover, not before.
- Legacy-side queue items that are not Temper work (e.g. the
  `project_search` query-log schema v2) stay tracked in the legacy plan.

## 1. Design discipline

The draft craft skill set at `~/work/skills/guild/craft` governs product
code here (AGENTS.md has the loading rule). What its spine means for Temper
concretely — this mapping is the repo's design contract:

**Functional units.** Every unit is one of three kinds, never an undeclared
mix; CLI verbs are orchestrators composing them.

| Kind | Temper instances |
|---|---|
| Pure computation | manifest + lock + catalog → rendered config text; the wall model (fraction × device memory + co-tenants + OS floor ≤ wired limit); render diffs (what loads/unloads, warmup cost); lock-drift computation; packet → catalog-row compilation |
| Read | hardware and allowance detection; upstream resolution (`update`); service status at its three levels (job loaded / process alive / answering with residency); lease state; catalog and provenance reads |
| Side effect | writing lock rows; installing rendered configs; the launchctl kick; lazy-pull downloads; writing state (active mode, leases); uninstall |

Corollaries: `status` never repairs; `check` never writes; `update` never
downloads or restarts. A verb that needs a new mix is a design question, not
an exception.

**Commit points.** Every mutating verb stages, validates, then commits once,
with irreversible effects ordered after the commit. This is already law in
the legacy generator (render to temp → parse-validate → atomic place); it
generalizes:

- `apply`: resolve gaps → stage lock rows + all renders → validate
  everything → commit lock + configs together. A failure before the commit
  leaves no change; a second `apply` changes nothing.
- `mode`: render the target posture → diff → commit the config swap → kick.
  The in-flight-request behavior is measured (M4 prerequisites) before this
  ships.
- `update`: writes pins only. Printing the acceptance gate is the design;
  running it is the human's move.

**Surface first.** The schemas and verb contracts (§2) are the commitment;
implementations — bash today, possibly Go later — are replaceable behind
them. Every schema gets a `data-modeling` design pass and owner review
before any code consumes it: the catalog and lock will outlive every
implementation. The Go-port oracle strategy (§4) exists only because of
this rule.

**Error taxonomy** (`unit-design`'s, applied to a CLI):

- **Business refusal** — lease held; a binding references an unselected
  item; an unqualified combination requested as qualified; budget exceeded
  → a printed explanation and a distinct nonzero exit code. Callers,
  including agents, branch on it; RESULT lines stay machine-parseable.
- **Operational failure** — resolve timeout, Hugging Face unreachable,
  llama-swap not answering → propagate with context; retry only what is
  transient, never a validation rejection.
- **Programming defect** — a render failing its own validation, a lock row
  without a hash → abort loudly. Never repair silently.

## 2. Interface contracts (the surfaces, in dependency order)

Each contract is designed and reviewed before code consumes it. "Writer"
follows the one-writer rule.

| # | Contract | Writer | Readers | Lands |
|---|---|---|---|---|
| C1 | rendered configs: llama-swap YAML + Pi `local` provider fragment | the generator | llama-swap, Pi | exists; frozen by the M0 oracle |
| C2 | `manifest.yaml` (named 2026-08-14 — it carries tools, harnesses and modes, not just models; the legacy file stays `models.yaml`). The full-layout home (owner, 2026-08-17): modes with their members, residency, engine flags/tuning, and the harness-derivation primitives per selected model | wizard once, then the user's hand | generator, `apply`, `check` | shape exists via legacy `models.yaml`; renamed when the wizard first writes it; `modes:` extension designed at M4 from the parked direction sketch (`docs/design/manifest-schema.md`, owner-reviewed 2026-08-17) |
| C3 | `manifest.lock.yaml` | `apply`/`update` only | generator, `check` | M1 |
| C4 | catalog profile records (six kinds) + the `WATCH/LAB/QUALIFIED/RETIRED/REJECTED` status machine | release review | wizard, `check`, render validation | M2 |
| C5 | Labs promotion packet | Labs review | the catalog compiler | M2, co-designed with Labs |
| C6 | state dir: active mode, leases | `mode`/`start`/`stop` | `mode`, `status`, cooperating harnesses | M4 |
| C7 | probe base: the reversible install/verify/remove mechanics plus the packet-identity handshake the field kit consumes (probe packets themselves are written by field-kit — owner boundary, 2026-08-14) | temper's base verbs | field-kit stages; Labs imports the kit's packets | M4, designed with field-kit |
| C8 | CLI verb surface: verbs, exit codes, RESULT lines, machine-parseable outcomes | this plan → per-verb design docs | humans and agents | grows M1 → M4 |

## 3. Milestones

### M0 — generator extraction (in the legacy repo)

> **Status 2026-08-14: code-complete, all gates green** (work items 2–4;
> uncommitted in the legacy worktree). `scripts/render-configs.sh` is the
> standalone executable — contract documented in its header (canonical
> until the executable itself moves here): args
> `<manifest> <swap-config-out> <pi-models-out> [pi-models-base]`, no env,
> exit 0/1/2 (rendered / usage-env error / manifest invalid), no
> detection, no network, deterministic. `steps/40-configs.sh` (346 → 143
> lines) keeps detection + staging + atomic place and calls it.
> `tests/render-oracle.sh` is the permanent oracle (determinism +
> pipeline-matches-direct on both manifests — deliberately no frozen
> golden for the live manifest, which changes with model intake); it runs
> as offline check 22 and the suite is 85/85. All four rendered artifacts
> byte-identical pre/post (SHA-256-verified); `--dry-run` clean; real-run
> gate first executed as `setup.sh --only configs && …` (scoped to avoid
> `launchctl` under the extraction session's constraints), then the
> **unrestricted `./setup.sh && ./setup.sh` ran on the owner's instruction
> later the same day: both passes `ok 33 · installed 0 · patched 0 ·
> changed 0 · skipped 4 · manual 0`, service loaded and answering — the
> second-run-clean gate is closed without deviation.** Item 1 done later
> the same day: Labs handoff **`config-renderer`** opened at `EXTRACTING`
> (`../labs/product-handoffs/config-renderer/`, full lifecycle logged,
> labs checks clean). Remaining: item 5 — the executable stays canonical
> legacy-side until M1 consumes it; the handoff moves to `SHIPPED` when it
> lands here. Go-port notes from the extraction (exact `, `-join spacing,
> 6-space folded-block indentation, literal `${PORT}`, validator field
> order, reliance on yq/jq insertion-order serialization) are in the M0
> session report and the script comments.

**Goal:** the render logic of `steps/40-configs.sh` (346 lines:
`render_swap_config`, `render_pi_models`, `kind_flags`, `group_members`,
validators) becomes one standalone executable with a documented contract;
`setup.sh` calls the *same executable* — two render implementations is the
two-sources-of-truth disease.

Work items:

1. *(process)* Open the product handoff in Labs (`kind:` Temper component,
   destination: this repo's generator); fill the product bar; reach
   `ACCEPTED`.
2. *(build, legacy)* Extract into a standalone script taking a manifest
   path and machine facts, emitting C1; `steps/40-configs.sh` keeps
   detection, staging, and the atomic place around the call.
3. *(build, legacy)* Byte-diff harness: render before/after on the live
   manifest **and** `models.example.yaml`; assert identical. The harness is
   kept — it is the future Go oracle.
4. *(gate, legacy)* Offline suite green (84 checks), shellcheck,
   second-run-clean, `--dry-run` purity. Announce before running the suite.
5. *(build, here)* Land the executable, its contract doc (inputs, outputs,
   exit codes, determinism guarantees), and the oracle harness; handoff →
   `SHIPPED`.

**Acceptance:** byte-identical configs on both manifests; legacy suite
green; the executable runs standalone with no repo context.
**Dependencies:** none. **Unblocks:** M1 `apply`, M3 wizard, M4's isolated
render trial (part of the probe base the field kit consumes).
**Decisions needed:** D12 confirmation only.

### M1 — lock + `apply` / `update` / `check`

> **Status 2026-08-14: started; schema draft radically trimmed after
> owner review.** [design/lock-schema.md](design/lock-schema.md) proposes
> `temper-lock/v1` as a five-field flat schema: per-entry
> `repo`(snapshot)/`revision`/`files`(+hashes)/`patches`/`resolved` —
> pins that never download, drift always computed. Everything else
> (machine block, `profiles:` catalogue,
> tool sections, mode-tuning records) is **deliberately absent**, listed
> with the milestone that adds it as a reviewed schema revision; the
> agreed directions from the review round (profiles list their entries;
> per-kind sections; tuning values never in the lock; mode-first
> manifest) are preserved there as directions, not structure. D13 closed
> 2026-08-18: no `attest` verb and no stored `verified` field — each
> temper release ships the database of tested versions (the M2 catalog)
> and `check` derives on/off-tested-set by comparison. The Go module is
> scaffolded (`go.mod` `github.com/temper-sh/temper`, Go 1.26;
> `cmd/temper` composition root; gofmt/vet/build clean; verbs refuse
> loudly until designed). No verb consumes the schema before the review.

**Goal:** pins and drift become file state instead of memory.

> Design input (2026-08-17, legacy FINDINGS #25): `apply` also materializes
> harness client settings that derive from the selected model window (SPEC:
> "Harness client settings are profile derivations") — per active
> mode/profile, derived, never constants. The legacy witness is Pi
> compaction sizing in `local-ai-setup/steps/40-configs.sh` section C.

Design first (a `data-modeling` pass, owner-reviewed, before any code):

1. *(design)* `manifest.lock.yaml` schema — designed and trimmed, see
   `docs/design/lock-schema.md` (the status note above). One home per
   fact: intent lives in the manifest, resolution in the lock, reviewed
   evidence in the catalog — the lock never restates evidence, and
   tested-status is `check`'s comparison against the release's catalog,
   never a lock field.
2. *(design)* Placement: beside the manifest in the current layout. The
   proposed `~/.temper` home (D3) moves manifest, lock, and state together
   if adopted — the schema must not care where it lives.

Then the verbs:

3. *(build)* The Go module and `cmd/temper` skeleton per
   `code-organization`'s Go reference (D2 resolved — §4), then `apply` —
   manifest + lock → configs by exec-ing the M0 executable (one render
   implementation until the native port, §4). Fills missing lock rows
   (resolve + pin), never moves existing pins; first apply after a wizard
   is the all-gaps case that creates the whole lock. Stage → validate →
   commit.
4. *(build)* Lazy-pull launcher: locking never forces downloads. Heavy
   entries get a generated launcher that fetches the locked revision on
   first start, replacing `-hf`-follows-a-remote-branch.
5. *(build)* `update [id]` — re-resolves upstream, prints old→new per
   entry, and ends by *printing*
   the targeted gate (coder: the streaming tool-call curl plus a plain
   completion; reranker: the magnitude probe) — never running it. From
   M2 it also warns when the new pin leaves the release's tested set. Per-entry
   is the normal move; bare `update` exists but bundles unrelated risk and
   says so.
6. *(build)* `check`, first slice — lock drift + budget arithmetic (the
   wall model; its output is always labeled a *prediction*). The advisory
   wizard-diff slice waits for M3's recommendation data.

**Acceptance:** hermetic tests with fixture manifests and canned
resolutions (no network); `apply && apply` — the second changes nothing;
`--dry-run` mutates nothing; `update` on a fixture moves exactly one row
and prints the right gate text; gofmt + go vet + table tests (the CLI is
Go — §4).
**Dependencies:** M0. **Decisions:** D3 any time before M3.

### M2 — catalog schema + promotion packet

**Goal:** "reviewed configuration" becomes a typed, validated artifact.

> Owner ruling 2026-08-18: the catalog is also **the per-release database
> of tested versions** — every temper release ships it, `check` compares
> the lock's pins against it for on/off-tested-set, and `update` warns
> when a move leaves it. No verified state is ever stored locally (D13
> closed on this).

1. *(decide, D1)* Representation: separate typed profile documents vs one
   normalized graph. **This plan's recommendation: separate typed documents**
   with shared identity conventions — hand-reviewable, and it is harder to
   make consent implicit in a document than in a graph edge. Revisit only if
   cross-profile queries hurt in practice.
2. *(design)* Schema per profile kind (spec: model artifact, engine, model
   runtime, tool, mode, activity) over a common envelope: exact pins,
   status, witness scope key (artifact revision × engine-profile revision ×
   runtime-profile revision × machine bucket × mode × co-residents),
   dependencies, known failures, data boundary, invalidation triggers, and
   a "what this means for you" line. **Applicability constraints join the
   envelope (owner, 2026-08-19):** a profile can declare the resource
   conditions under which it is useful *at all* — the live case is the Pi
   extensions born from the constrained-window experiments
   (`compaction-guard`, `context-trim`), which earn their place beside a
   16k local window and are noise beside a frontier one. Distinct from
   witness scope: scope records where the evidence *is*, applicability
   records where the thing has a reason to *exist*; the wizard clips its
   offers by it (M3). Roles are part of this envelope: a
   tool profile declares the role it consumes (`rerank`, `extract`), and a
   mode profile binds roles to qualified runtime profiles — roles are the
   stable interface harnesses speak; the mode decides the mapping. Runtime
   profiles are deliberately plural per artifact; compatibility witnesses
   may be shared, fit/stability/cache/performance witnesses may not.
3. *(design, with Labs)* The promotion packet (C5): how a reviewed Labs
   packet compiles into a catalog row without becoming consent.
   `field-kit-runtime-profile/v1` maps in as the exploratory-witness
   special case; `external-lab` packets stay inspectable but outside the
   generic install path.
4. *(build)* Catalog validator: well-formedness, status-machine legality,
   witness-scope completeness, consent-neutrality (no row can mark itself
   selected). The render-time rule is enforced from here on: a binding
   referencing an unselected item, or an unqualified combination presented
   as qualified, fails the render.
5. *(build)* Seed catalog: compile the current stack's already-reviewed
   truth (the live posture: coder + on-demand reranker, the sampling
   adoption, the GPU-pool rules) into the first `QUALIFIED` rows —
   witnessed only for this machine's bucket, and saying so. Also compile
   Labs handoff `qwen38-native-mtp-profile` as the first consent-neutral
   experimental row: Qwen3.8 is the intended intelligence target, but the
   packet must remain `LAB`, opt-in, non-default and non-recommended, with
   its autonomous M5/32 GiB scope retained as `REJECTED`. The packet is a
   C5 fixture/input, not permission to read Labs at runtime or alter the
   legacy live service.

**Acceptance:** a round-trip fixture (fake packet → row → wizard-readable
record); the validator rejects each illegal fixture; seeded rows carry
complete witness scope.
**Dependencies:** M1 (the lock cites catalog revisions). Labs-side parity
(`add-tool` intake) is Labs work, tracked there.
**Decisions:** D1 blocks item 2; D7 (invalidate vs fork a witnessed row on
engine update) is settled inside the schema design.

### M3 — wizard

**Goal:** the choice surface — the spec's seven steps, advisory on re-run.

1. *(build)* The TUI foundation: bubbletea/huh over the catalog reader
   (D2 resolved — §4). Distribution (D4) is worth deciding by here so the
   toolchain and release shape stop being hypothetical.
2. *(build)* The flow per spec: detect hardware/allowances → model universe
   → tools one-by-one with backend, data-boundary, and dependency
   consequences shown before selection, the offer list clipped by each
   profile's applicability constraints (M2 envelope — a
   constrained-machine extension is not offered to an unconstrained
   machine) → harness integrations for detected
   harnesses → mode templates clipped to selections → preview (downloads,
   disk, memory, mode transitions, external data paths) → write
   `manifest.yaml` once, render, probe. Tools start unselected;
   recommendations are explanations, never checked boxes.
3. *(build)* Advisory re-run: manifest present → print divergence from the
   current bucket recommendation, stop. No interactive stage under
   `--dry-run`, no second-run mutation — "written once" buys both.
4. *(build)* The M0 oracle gates every render path: byte-identical
   configs or no cutover. `code-organization`'s Go reference governs layout
   (`cmd/` composition root, `internal/` packages); wiring lives at the
   root, families construct their own members. If the native renderer port
   (§4) has not landed by now, the wizard's preview execs the M0
   executable like `apply` does.

**Acceptance:** the wizard on a fixture machine profile produces a manifest
that `apply` renders byte-identically to its hand-written equivalent; a
consent audit finds no path from recommendation to selection without an
explicit choice; re-run is advisory only.
**Dependencies:** M2 (the catalog is what it browses), M1, M0.
**Decisions:** D3 (where the wizard writes), D8 (remote providers
render-only), D4 (latest useful point).

### M4 — probe base + the mode state machine

**Boundary (owner, 2026-08-14).** Probes belong to the field kit; Temper
installs the basic requirements and setup. There is no `temper probe`, and
the field kit does not become a shim — it stays the standalone witness
surface, orchestrating its stages, RESULT lines, packets, consent gates and
keep-or-restore over the reversible base Temper provides (field-kit v2's
"minimum Temper probe base": canonical machine facts, provenance, llama-swap
and basic dependencies, isolated profile rendering, service lifecycle,
artifact verification, and removal of only what a probe run added).

**Labs prerequisites** *(measure — experiments run in Labs, before any mode
binding ships as qualified)*:

- llama-swap config-reload under in-flight requests: does `--watch-config`
  unload a resident coder cleanly, or does the switch need an explicit
  unload call? Measure, don't assume.
- mode-switch latency and warmup cost, per the render diff's prediction.
- role-alias mechanics (harnesses speak `rerank`; the mode binds it).
- lease semantics under two live cooperating harnesses.

Work items:

1. *(design + build, with field-kit)* The probe-base surface: expose the
   reversible mechanics above as stable, machine-parseable commands —
   scoped install of basic requirements, isolated render trial, service
   lifecycle, artifact verification, machine facts, provenance-guided
   removal — and design the field-kit ↔ temper interface together: which
   commands, exit codes, and RESULT-friendly output the kit's stages
   consume (`../labs/field-kit/README.md` is the contract). Temper's side
   of the packet handshake stays here too: verifying and stamping
   `field-kit-runtime-profile/v1` identity once the selected manifest is
   active. Re-witnessing the owner's own box after an update is a
   field-kit run against this base.
2. *(build)* Mode machinery: `mode <name>` (render → diff → lease check →
   commit swap → kick; reports loads/unloads and warmup cost);
   `start`/`stop`/`status` as the off-mode transitions of the same state
   machine. `mode --request <name>` is the harness-facing form: it
   succeeds only lease-free and never preempts — the verb a harness calls
   when it notices its coder idle; `--force` stays human-only (item 3).
   `status` distinguishes job loaded / process alive / answering
   with residency; `stop` prints the wired memory it freed.
3. *(build)* Leases (C6): an advisory state file (harness, mode, expiry),
   renewed while active; `temper mode` honors live leases; `--force` stays
   human-only. Idle detection lives in harnesses — no watcher here, the
   no-daemon rule holds. The harness side of the cooperation — renewing
   the lease while active, calling `mode --request` on idle or on a
   coder-model switch (Pi's case) — ships with each harness adapter, and
   its protocol test is part of the two-live-harnesses prerequisite above.
4. *(build)* `report` (status-snapshot paste-block — probe reports are
   field-kit artifacts) and `uninstall` (provenance-guided; the grammar is
   already shared with field-kit and the legacy `scripts/uninstall.sh`).
5. *(design + build)* Mode layouts land in the manifest schema (C2
   extension) and the generator. **Direction (owner, 2026-08-14):
   mode-first, not per-entry overlays** — the manifest gets a `modes:`
   section where each mode lists its members (models, tools) and their
   residency/tuning bindings in one place, so the layout is readable at
   the mode, mirroring the lock's profiles-as-catalogue inversion.
   **Scope (owner, 2026-08-17, after legacy FINDINGS #25):** the schema
   reflects the *whole* layout — models, engine flags/tuning, tools, and
   the harness side. Each mode's model binding carries the primitives
   (window, generation budget) from which harness client settings derive
   (SPEC: "Harness client settings are profile derivations"), so a mode
   switch re-materializes e.g. Pi's compaction sizing for the tightest
   selected window; derived values are rendered, never stored. The
   generator renders per-mode
   artifacts; a render fails on bindings outside the wizard set. A
   missing required role makes the mode invalid or the tool visibly
   unavailable — never silently substituted.

**Acceptance:** a field-kit run on a temper-installed base reproduces the
kit's current results end-to-end; a mode switch is witnessed under load per
the prerequisite experiments; an uninstall round-trip on a scratch render
removes exactly what was added; second-run-clean throughout.
**Dependencies:** M0–M3, plus the Labs prerequisites above.
**Decisions:** D6 (lease semantics), D10 (adapter distribution channels),
D11 (which modes ship qualified). D5 moved to the field-kit/Labs side —
see the register.

### M5 — release

**Goal:** `temper-sh/temper` public, the live machine cut over, the org
story complete.

1. *(gate)* Cutover: Temper reaches parity for this machine's posture —
   `apply` renders what legacy renders (oracle), a field-kit run on the
   temper-installed base passes, the acceptance suite is green. Then the live box moves to Temper as its
   first witness machine and legacy `setup.sh` freezes. Owner-scheduled;
   announced; reversible until the freeze.
2. *(decide, D4)* Distribution: brew formula vs curl-installer vs
   release-asset binary. Third-party notices ride the release asset; the
   tree stays 0BSD-clean either way.
3. *(build)* Zero-context docs pass: README, compact applicability
   references, "FINDINGS #N" citations replaced by stable Results records
   or compact release anchors, "last reviewed" watermarks.
4. *(build, with Labs + Results)* Review wiring: promotion publishes a
   Results record and compiles a catalog row — two explicit outputs, one
   writer each, no live cross-repo dependency. The catalog ships only
   reviewed qualified rows.
5. *(process)* Legacy repo archived per Labs' archive-migration job (Labs
   work; original revisions and hashes preserved before layout improves).
6. *(build)* CI per the org roster bar: offline, hermetic, per-repo.

## 4. The language decision (D2) — resolved: Go

**Decided 2026-08-14 (owner):** the wizard will be TUI-heavy with
certainty, so by the spec's split-brain rule ("a binary that owns only the
TUI while bash owns `apply`/`update` is a split brain") the whole CLI is
Go. Implementing M1's verbs in bash and reimplementing them at M3 would be
building the product twice; the earlier bash-through-M1 option is dropped.

What this does *not* change: **nothing gets written twice.**

- **M0 stays bash and stays first.** The render logic already exists in
  the legacy repo; M0 extracts it (a refactor, not new code), defines the
  render interface, and becomes the byte-diff oracle — a role it was
  needed for regardless of language.
- **M1's Go CLI execs the M0 renderer at first.** One render
  implementation during the transition: `apply` orchestrates in Go and
  calls the same extracted executable legacy `setup.sh` calls. Verbs,
  lock handling, and validation are written once, in Go.
- **The native Go renderer is its own oracle-gated work item** — any time
  after M1, and before the M5 release at the latest: a shipped static
  binary should be self-contained, so a dev-phase exec of a bash script is
  acceptable and a released one is not. The gate is byte-identity on the
  fixture manifests and the live one; the oracle harness then stays in CI
  as a regression fence permanently.
- **bash 3.2 still governs scripts** — rendered launchers, machine-report,
  anything the generator emits that llama-swap or launchd executes, and
  everything legacy-side. That ground rule was never about the CLI.

The remaining Go sub-decision folds into D4 (distribution): prebuilt
darwin/arm64 release asset vs building at setup time (the latter adds the
Go toolchain as a brew dependency); the tree stays 0BSD-clean either way,
since release *assets* carry the third-party notices. bubbletea/huh is the
presumed toolkit; llama-swap being Go is a familiarity argument, not a
dependency. Layout follows `code-organization`'s Go reference: `cmd/` is
the composition root, `internal/` packages, and the exported surface is
exactly the contracts in §2.

## 5. Quality bars and CI

Inherited release bar, enforced from the first commit of product code:

- shellcheck-clean bash 3.2 for anything shell;
- the hermetic offline suite is the only per-change gate; heavy/runtime
  verification is on-demand and announced;
- second-run-clean; `--dry-run` purity; machine-derived values only (never
  hardcode one box's numbers);
- tests never touch launchd, the live service, or the network;
- no sudo; no daemon; no telemetry; 0BSD tree, no vendored third-party
  files;
- quality outranks throughput: any model/engine claim measures
  first-attempt task success first, tok/s second, with conditions on every
  number.

Go (the CLI — §4): gofmt + go vet + table tests; the oracle harness runs
in CI from the first native render path, diffing Go renders against the
pinned bash generator, and stays as a regression fence after cutover.

## 6. Decision register (owner)

| # | Decision | Blocks | Current lean |
|---|---|---|---|
| D1 | Catalog representation: typed documents vs normalized graph | M2 | typed documents (§3/M2) |
| D2 | Language | — | **resolved 2026-08-14: the whole CLI is Go** (the wizard is certainly TUI-heavy; the split-brain rule pulls the rest — §4). Open remainder: native-renderer-port timing (after M1, before M5) |
| D3 | Adopt `~/.temper` as the machine-identity home | M3 (wizard write location); M1 schema stays location-neutral | spec proposes yes |
| D4 | Distribution: brew vs curl-installer vs release asset (now carries the Go sub-decision: prebuilt darwin/arm64 asset vs build-at-setup) | M5; useful by M3 | open |
| D5 | Mode-posture soaks as field-kit experiment packages (was `probe --mode`; probes are field-kit's per the 2026-08-14 boundary — temper only guarantees the base can render and serve the requested posture in isolation) | M4 qualification | open — a field-kit/Labs question (spec Q8) |
| D6 | Advisory lease file with expiry sufficient; `--force` human-only | M4 | leaning yes on both (spec Q7) |
| D7 | Witnessed-row versioning on engine update: invalidate vs fork | inside M2's schema design | open (spec Q5) |
| D8 | Remote-provider integration strictly render-only | M3 | leaning yes (spec Q4) |
| D9 | Catalog contribution flow for foreign witnesses | post-M5 | hand-curated by the owner (current stance) |
| D10 | Pi `packages` / Codex / Claude Code plugin packaging as distribution channels | M4 adapters | open (spec Q9) |
| D11 | Which of research/docs, planning, coding, helper ship as qualified v1 modes | M4/M5 | evidence-driven, open (spec Q2) |
| D12 | Development locus stance (§0: M0 in legacy, M1+ here, live cutover at M5) | — | confirmed in practice 2026-08-14 (owner directed M0 in legacy) |
| D13 | Who records `witness: verified` — a small `attest` verb vs some other mechanism; `check` must stay a pure read | M1 verbs | **closed 2026-08-18**: nobody — no local verified state at all; each release ships the tested-versions database (the M2 catalog) and `check` derives it by comparison |

## 7. Now / next

1. **Owner:** review this plan and the adopted `SPEC.md`; confirm D12; say
   when M0 may start.
2. **M0:** open the Labs handoff record, then run the legacy-repo
   extraction (announced before its suite runs).
3. **In parallel, machine-free design work:** draft the lock schema (M1
   item 1) and the catalog envelope (M2 item 2) for owner review — both are
   pure design and block nothing by being early.
4. **Labs:** queue the M4 prerequisite experiments (config-reload under
   in-flight load, mode-switch latency) through the new-experiment workflow
   when convenient — they gate mode qualification, not M0–M3.
