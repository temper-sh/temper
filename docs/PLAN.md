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
| C2 | `models.yaml` manifest; per-mode overlays later | wizard once, then the user's hand | generator, `apply`, `check` | exists; overlay extension at M4 |
| C3 | `models.lock.yaml` | `apply`/`update` only | generator, `check` | M1 |
| C4 | catalog profile records (six kinds) + the `WATCH/LAB/QUALIFIED/RETIRED/REJECTED` status machine | release review | wizard, `check`, render validation | M2 |
| C5 | Labs promotion packet | Labs review | the catalog compiler | M2, co-designed with Labs |
| C6 | state dir: active mode, leases | `mode`/`start`/`stop` | `mode`, `status`, cooperating harnesses | M4 |
| C7 | probe packet (`field-kit-runtime-profile/v1` today) | `probe` | Labs import | exists labs-side; consumed at M4 |
| C8 | CLI verb surface: verbs, exit codes, RESULT lines, machine-parseable outcomes | this plan → per-verb design docs | humans and agents | grows M1 → M4 |

## 3. Milestones

### M0 — generator extraction (in the legacy repo)

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
**Dependencies:** none. **Unblocks:** M1 `apply`, M3 wizard, M4 probe's
isolated render trial.
**Decisions needed:** D12 confirmation only.

### M1 — lock + `apply` / `update` / `check`

**Goal:** pins and drift become file state instead of memory.

Design first (a `data-modeling` pass, owner-reviewed, before any code):

1. *(design)* `models.lock.yaml` schema: per-entry artifact pins (source,
   revision, filename, SHA-256), the catalog-profile revision used, witness
   metadata (engine and harness versions, hardware bucket, conditions,
   corpus, acceptance date), witness status (`verified`/`unverified`). One
   home per fact: intent lives in the manifest, resolution in the lock,
   reviewed evidence in the catalog — the lock cites catalog revisions,
   never restates evidence.
2. *(design)* Placement: beside `models.yaml` in the current layout. The
   proposed `~/.temper` home (D3) moves manifest, lock, and state together
   if adopted — the schema must not care where it lives.

Then the verbs:

3. *(build)* `apply` — manifest + lock → configs via the M0 executable.
   Fills missing lock rows (resolve + pin), never moves existing pins;
   first apply after a wizard is the all-gaps case that creates the whole
   lock. Stage → validate → commit.
4. *(build)* Lazy-pull launcher: locking never forces downloads. Heavy
   entries get a generated launcher that fetches the locked revision on
   first start, replacing `-hf`-follows-a-remote-branch.
5. *(build)* `update [id]` — re-resolves upstream, prints old→new per
   entry, resets touched witnesses to `unverified`, and ends by *printing*
   the targeted gate (coder: the streaming tool-call curl plus a plain
   completion; reranker: the magnitude probe) — never running it. Per-entry
   is the normal move; bare `update` exists but bundles unrelated risk and
   says so.
6. *(build)* `check`, first slice — lock drift + budget arithmetic (the
   wall model; its output is always labeled a *prediction*). The advisory
   wizard-diff slice waits for M3's recommendation data.

**Acceptance:** hermetic tests with fixture manifests and canned
resolutions (no network); `apply && apply` — the second changes nothing;
`--dry-run` mutates nothing; `update` on a fixture flips witness state and
prints the right gate text; bash 3.2 + shellcheck (unless D2 lands Go
early — §4).
**Dependencies:** M0. **Decisions:** D3 any time before M3; D2 affects
implementation language only, not these contracts.

### M2 — catalog schema + promotion packet

**Goal:** "reviewed configuration" becomes a typed, validated artifact.

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
   a "what this means for you" line. Runtime profiles are deliberately
   plural per artifact; compatibility witnesses may be shared, fit/
   stability/cache/performance witnesses may not.
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
   witnessed only for this machine's bucket, and saying so.

**Acceptance:** a round-trip fixture (fake packet → row → wizard-readable
record); the validator rejects each illegal fixture; seeded rows carry
complete witness scope.
**Dependencies:** M1 (the lock cites catalog revisions). Labs-side parity
(`add-tool` intake) is Labs work, tracked there.
**Decisions:** D1 blocks item 2; D7 (invalidate vs fork a witnessed row on
engine update) is settled inside the schema design.

### M3 — wizard

**Goal:** the choice surface — the spec's seven steps, advisory on re-run.

1. *(decide, D2)* Go scope + timing first; it decides what the wizard is
   written in (§4 has the framing and recommendation).
2. *(build)* The flow per spec: detect hardware/allowances → model universe
   → tools one-by-one with backend, data-boundary, and dependency
   consequences shown before selection → harness integrations for detected
   harnesses → mode templates clipped to selections → preview (downloads,
   disk, memory, mode transitions, external data paths) → write
   `models.yaml` once, render, probe. Tools start unselected;
   recommendations are explanations, never checked boxes.
3. *(build)* Advisory re-run: manifest present → print divergence from the
   current bucket recommendation, stop. No interactive stage under
   `--dry-run`, no second-run mutation — "written once" buys both.
4. *(build, if Go)* The M0 oracle gates every render path: byte-identical
   configs or no cutover. `code-organization`'s Go reference governs layout
   (`cmd/` composition root, `internal/` packages); wiring lives at the
   root, families construct their own members.

**Acceptance:** the wizard on a fixture machine profile produces a manifest
that `apply` renders byte-identically to its hand-written equivalent; a
consent audit finds no path from recommendation to selection without an
explicit choice; re-run is advisory only.
**Dependencies:** M2 (the catalog is what it browses), M1, M0.
**Decisions:** D2, D3 (where the wizard writes), D8 (remote providers
render-only).

### M4 — probe absorption + the mode state machine

**Labs prerequisites** *(measure — experiments run in Labs, before any mode
binding ships as qualified)*:

- llama-swap config-reload under in-flight requests: does `--watch-config`
  unload a resident coder cleanly, or does the switch need an explicit
  unload call? Measure, don't assume.
- mode-switch latency and warmup cost, per the render diff's prediction.
- role-alias mechanics (harnesses speak `rerank`; the mode binds it).
- lease semantics under two live cooperating harnesses.

Work items:

1. *(build)* `temper probe [stage]` absorbing field-kit v2's baseline
   contract (`../labs/field-kit/README.md`): machine report, identity
   checks, endpoint/streaming/tool-call checks, growing-context soak,
   timing, hash ledger, consent gates, keep-or-restore. On the owner's box
   it re-witnesses after an update; on a friend's box it does what
   field-kit does today. The field-kit repo becomes a shim over
   `temper probe` at v1 so "send one file first, then one clone" survives
   the rename. Temper owns the reversible mechanics field-kit's contract
   assigns it: canonical machine facts, provenance, llama-swap, isolated
   profile rendering, service lifecycle, artifact verification, removal of
   only what the probe added.
2. *(build)* Mode machinery: `mode <name>` (render → diff → lease check →
   commit swap → kick; reports loads/unloads and warmup cost);
   `start`/`stop`/`status` as the off-mode transitions of the same state
   machine. `status` distinguishes job loaded / process alive / answering
   with residency; `stop` prints the wired memory it freed.
3. *(build)* Leases (C6): an advisory state file with expiry, renewed while
   active; `temper mode` honors live leases; `--force` stays human-only.
   Idle detection lives in harnesses — no watcher here, the no-daemon rule
   holds.
4. *(build)* `report` (current paste-block) and `uninstall`
   (provenance-guided; the grammar is already shared with field-kit and the
   legacy `scripts/uninstall.sh`).
5. *(design + build)* Mode overlays land in the manifest schema (C2
   extension) and the generator: entries carry an artifact base plus
   `modes:` runtime-profile bindings; the generator renders per-mode
   artifacts; a render fails on bindings outside the wizard set.

**Acceptance:** `probe` on this machine reproduces the field kit's current
results; a mode switch is witnessed under load per the prerequisite
experiments; an uninstall round-trip on a scratch render removes exactly
what was added; second-run-clean throughout.
**Dependencies:** M0–M3, plus the Labs prerequisites above.
**Decisions:** D5 (`probe --mode`), D6 (lease semantics), D10 (adapter
distribution channels), D11 (which modes ship qualified).

### M5 — release

**Goal:** `temper-sh/temper` public, the live machine cut over, the org
story complete.

1. *(gate)* Cutover: Temper reaches parity for this machine's posture —
   `apply` renders what legacy renders (oracle), `probe` passes, the
   acceptance suite is green. Then the live box moves to Temper as its
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

## 4. The language decision (D2), framed

The spec settled the shape ("if Go enters, the whole CLI likely follows;
the bash generator extraction goes first regardless and becomes the
oracle"). What remains is timing and distribution:

- **Option A — bash through M1, decide Go at M3.** M1's verbs are thin
  orchestration over the M0 executable, so bash keeps the running reference
  trustworthy and defers the toolchain question until the wizard's TUI
  actually forces it. Cost: a later port of `apply`/`update`, bounded by
  the hermetic tests and the oracle.
- **Option B — Go from M1.** One implementation, no port; cost: the Go
  renderer must reach byte-identity with the bash oracle before anything
  ships, and the toolchain question arrives before the TUI needs it.

**Recommendation: Option A.** Surface-first is what makes it cheap — the
contracts are identical either way, and the oracle harness converts the
port from a risk into a diff. The two Go sub-decisions when the time comes:
distribution (prebuilt darwin/arm64 release asset vs building at setup
time, which adds the Go toolchain as a brew dependency) and scope (the
whole CLI, per the split-brain rule). bubbletea/huh is the presumed
toolkit; llama-swap being Go is a familiarity argument, not a dependency.

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

If Go enters: gofmt + go vet + table tests; the oracle harness runs in CI,
diffing Go renders against the pinned bash generator until cutover, and as
a regression fence after.

## 6. Decision register (owner)

| # | Decision | Blocks | Current lean |
|---|---|---|---|
| D1 | Catalog representation: typed documents vs normalized graph | M2 | typed documents (§3/M2) |
| D2 | Language: Go scope + timing | M3; shapes M1's medium | bash through M1, decide at M3 (§4) |
| D3 | Adopt `~/.temper` as the machine-identity home | M3 (wizard write location); M1 schema stays location-neutral | spec proposes yes |
| D4 | Distribution: brew vs curl-installer vs release asset | M5 | open |
| D5 | `probe --mode` as a first-class surface | M4 | open (spec Q8) |
| D6 | Advisory lease file with expiry sufficient; `--force` human-only | M4 | leaning yes on both (spec Q7) |
| D7 | Witnessed-row versioning on engine update: invalidate vs fork | inside M2's schema design | open (spec Q5) |
| D8 | Remote-provider integration strictly render-only | M3 | leaning yes (spec Q4) |
| D9 | Catalog contribution flow for foreign witnesses | post-M5 | hand-curated by the owner (current stance) |
| D10 | Pi `packages` / Codex / Claude Code plugin packaging as distribution channels | M4 adapters | open (spec Q9) |
| D11 | Which of research/docs, planning, coding, helper ship as qualified v1 modes | M4/M5 | evidence-driven, open (spec Q2) |
| D12 | Development locus stance (§0: M0 in legacy, M1+ here, live cutover at M5) | M0 start | this plan's stance — confirm |

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
