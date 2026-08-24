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
| `../labs` | authors experiments, promotes bounded experiments, decides, and gathers evidence | reviewed profile packets, accepted product handoffs; Field Kit promotions do not enter Temper |
| `../results` | explains reviewed evidence to people | nothing at runtime; shared evidence identifiers |
| `../field-kit` | agent-operated catalog of immutable Labs-promoted experiments for consenting users | witness reports, via Labs review |
| `../local-ai-setup` (legacy) | running reference implementation + evidence history | behavior reference and optional comparison oracle; no runtime component lands here |
| **this repo** | ships reviewed configuration + the minimum probe environment | — |

Work enters this repo through exactly two doors:

1. **Product handoffs** from Labs: a `PROPOSED → ACCEPTED` record in
   `../labs/product-handoffs/` with the product bar filled in. The
   destination (here) owns the extraction or rewrite; prototypes are
   rewritten to this repo's bar, never copied because they worked once.
2. **Product engineering planned here**: schemas, CLI verbs, wizard, CI —
   work that was never an experiment.

**Development locus (stance D12, revised by owner 2026-08-19).** The legacy
extraction was completed where the live code lives, which gave us a useful
behavior reference. It does not become a Temper runtime dependency:

- **M0's Bash executable stays in legacy.** Porting the old manifest and its
  raw flags into this repo would build a migration layer for an interface no
  Temper user has. It may be run externally as a comparison oracle while
  equivalence is useful; it is neither shipped nor exec'd by `temper`.
- **The native manifest, lock, renderer, and `apply` are developed together
  here.** During bootstrap the owner or Labs maintains the two input files
  manually; resolution and the wizard can automate that later without
  changing the renderer.
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
| Pure computation | manifest + lock + qualification catalog → rendered config text; software candidates + supply policy → exact software-lock selection; the wall model (fraction × device memory + co-tenants + OS floor ≤ wired limit); render diffs (what loads/unloads, warmup cost); lock-drift computation; packet → catalog-row compilation |
| Read | hardware and allowance detection; provider-native upstream resolution; service status at its three levels (job loaded / process alive / answering with residency); lease state; catalog, installed-software, and provenance reads |
| Side effect | writing lock rows; installing exact software from a software lock; installing rendered configs; the launchctl kick; lazy-pull downloads; writing state (active mode, leases); uninstall |

Corollaries: `status` never repairs; `check` never writes; `update` never
downloads or restarts. A verb that needs a new mix is a design question, not
an exception.

**Commit points.** Every mutating verb stages, validates, then commits once,
with irreversible effects ordered after the commit. This is already law in
the legacy generator (render to temp → parse-validate → atomic place); it
generalizes:

- `resolve`: read manifest + existing lock → resolve only missing rows → stage
  and validate the whole candidate lock → atomically replace the lock once.
  Existing rows never move; concurrent lock change causes a refusal and rerun.
- `apply`: read and validate the manifest and complete lock → render a complete
  immutable generation → atomically move `rendered/current`. A refusal leaves
  the selected world unchanged; a second identical `apply` changes nothing.
- `fetch <layout>`: download and verify one exact layout into a sibling stage,
  then atomically publish its content-addressed artifact-set directory once.
  A multi-GB network effect is always explicitly scoped.
- `mode`: render the target posture → diff → commit the config swap → kick.
  The in-flight-request behavior is measured (M4 prerequisites) before this
  ships.
- `update`: writes pins only. Printing the acceptance gate is the design;
  running it is the human's move.
- software resolution/update: read provider candidates → select with the
  catalog's provider-native policy → stage and validate the complete
  `software.lock.yaml` → atomically replace it once. It never installs.
- software install: read a complete software lock plus its required base
  receipts → compute the whole named-installation/provider/claim plan →
  atomically prepare root-wide intent and claims → perform declared adapter
  effects → inspect actual state → publish the per-installation receipt →
  atomically finalize claims. Isolated adapters publish below one named root;
  shared adapters reconcile through one root-wide authority and may never
  claim ownership they cannot prove.

**Surface first.** The schemas and verb contracts (§2) are the commitment;
the native Go internals remain replaceable behind them. Every schema gets a
`data-modeling` design pass and owner review before code consumes it: the
catalog and lock will outlive every implementation. Legacy comparison tests
are evidence about behavior, not authority over the new surface.

**Error taxonomy** (`unit-design`'s, applied to a CLI):

- **Business refusal** — lease held; a binding references an unselected
  item; an unqualified combination requested as qualified; budget exceeded
  → a printed explanation, a stable problem code, and a nonzero exit. Callers,
  including agents, branch on it; RESULT lines stay machine-parseable.
- **Operational failure** — resolve timeout, Hugging Face unreachable,
  llama-swap not answering → propagate with context; retry only what is
  transient, never a validation rejection.
- **Programming defect** — a render failing its own validation, a lock row
  without a hash → abort loudly. Never repair silently.

### Secondary objective through 1.0 — field-test the craft skills

Temper is the craft set's first sustained real-product test. Product delivery
and safety remain the primary objective; through the M5/1.0 release, the
secondary objective is to evaluate the skills against the work they actually
helped shape and propose narrowly evidenced improvements where warranted.

The closeout points are the M1 retrospective baseline, M2 phases A/B/C, M3,
M4, and M5/1.0. Before one of those phases is called complete, add a short
entry to `docs/craft-skill-field-notes.md` that records:

- which skills materially influenced the phase and the concrete decisions or
  artifacts where that influence appears;
- what guidance was useful, missing, misleading, over-specific, or costly to
  apply;
- any defect or near miss the guidance exposed or failed to expose; and
- a narrow proposed improvement, its owning skill/seam, and its evidence — or
  an explicit conclusion that no change is warranted.

This is a field note, not a model eval. It adds no synthetic implementation
work, model call, network access, or heavy test run to a Temper phase. One
Temper incident may support a regression scenario or proposal but does not by
itself become a universal craft rule. Skill edits, routing-description changes,
and new-skill liveness remain separate work under the skills repository's own
review and user gates. The M5 note closes the objective by recommending
whether this practice should continue after 1.0.

## 2. Interface contracts (the surfaces, in dependency order)

Each contract is designed and reviewed before code consumes it. "Writer"
follows the one-writer rule.

| # | Contract | Writer | Readers | Lands |
|---|---|---|---|---|
| C1 | rendered configs: llama-swap YAML, Pi `local` provider, Pi compaction settings | native renderer | llama-swap, Pi | M1 first slice; exact bytes pinned by unit goldens |
| C2 | `manifest.yaml`: layouts `(model, engine, tuning)` plus modes (members, residency, tools, harnesses) | wizard once, then the user's hand; manually maintained during bootstrap | renderer, `apply`, `check` | `temper-manifest/v1` executable in M1; see `docs/design/manifest-schema.md` |
| C3 | `manifest.lock.yaml` | `resolve` for missing rows; future `update` for moving pins | renderer, `fetch`, `apply`, `check` | `temper-lock/v1` executable in M1 |
| C4 | software supply records for Temper-managed packages: logical package, portable installation method, target-adapter definition, adapter-native package recipe/version scheme, selection policy, constraints, and tested-version evidence | release review | software resolver, installer, `check` | M2 phase A |
| C5 | `software.lock.yaml`: exact target/method/adapter/provider closure, immutable catalog and/or experiment provenance, and required base-lock identities | explicit catalog resolution/update or explicit experiment-lock generation | installer, `check`, Field Kit Temper-material binding | M2 phase A |
| C6 | named installation receipt plus root-wide software state: observed closure/base receipts and current prepared operations/shared claims | installer around inspected effects and receipt commit | `check`, uninstall, Field Kit Temper-material binding | M2 phase B |
| C7 | qualification profiles (model artifact, engine, model runtime, tool, mode, activity) + the `WATCH/LAB/QUALIFIED/RETIRED/REJECTED` status machine | release review | wizard, `check`, render validation | M2 phase C; Temper-only surface provisionally approved 2026-08-25 in `docs/design/qualification-catalog-schema.md` |
| C8 | Labs product-promotion packet | Labs review | the qualification-catalog compiler | M2 phase C; Temper side provisionally approved 2026-08-25 in `docs/design/product-promotion-contract.md`, Labs adoption pending |
| C9 | state dir: active mode, leases | `mode`/`start`/`stop` | `mode`, `status`, cooperating harnesses | M4 |
| C10 | Field Kit execution base: reversible install/check/remove mechanics plus canonical machine facts and the ordered installation-set binding (experiment packages and sessions remain Field Kit-owned) | Temper's software and machine verbs | Field Kit experiment prompts; Labs imports reviewed run packets | M2 phase B, designed with Field Kit |
| C11 | CLI verb surface: verbs, exit codes, RESULT lines, machine-parseable outcomes | this plan → per-verb design docs | humans and agents | grows M1 → M4 |
| C12 | Labs-to-Field-Kit experiment promotion: immutable experiment identity, machine applicability/buckets, consent and cost envelope, bounded adaptive prompt, evidence shape, and invalidation/retirement policy | Labs review | Field Kit root catalog and experiment prompts; Temper is not a reader | Field Kit rebuild, co-designed in Labs and Field Kit after M2's Temper boundary |

## 3. Milestones

### M0 — legacy extraction (reference completed; product landing retired)

> **Disposition 2026-08-19 (owner): do not port the extracted Bash
> renderer.** The legacy refactor and its 85/85 oracle were useful: they made
> current behavior inspectable and proved the live installer stayed clean.
> Temper, however, has no installed base whose old manifest must remain
> compatible. Shipping the script here would spend a product slice preserving
> raw flags and serialization accidents that the new manifest intentionally
> removes.

The standalone legacy renderer and oracle remain in `../local-ai-setup` as
read-only comparison material. They can answer “did the native output retain
this useful behavior?” but cannot require byte identity where Temper is
deliberately better—for example, Temper uses the lock to render an exact local
model path with `--offline` instead of a moving `-hf` reference.

No M0 runtime artifact lands in this repository, and M1 has no dependency on
the Bash executable. The Labs handoff records the historical extraction and
this disposition; it is not a package Temper waits to ship.

### M1 — native manifest + lock + `resolve` / `fetch` / `apply` / `update` / `check`

> **Status 2026-08-20: M1 complete; five native vertical slices implemented.**
> `temper-manifest/v1` and `temper-lock/v1` are strict executable schemas;
> the pure Go renderer produces llama-swap and Pi artifacts directly; and
> `temper apply` stages a content-addressed generation then atomically moves
> `rendered/current`. `temper resolve` fills missing rows through one atomic
> lock commit without moving existing pins, while `temper fetch <layout>`
> publishes one verified content-addressed artifact set. Apply now admits a
> selected layout only through fetch's shared receipt/shape verifier, and every
> v1 text-only server command carries `--no-mmproj`. `temper check` reports
> whole-lock drift plus selected-mode artifact admission, while `--verify`
> streams every selected file against its lock SHA-256. Its wall-model slice
> reads macOS memory facts and admitted resident model sizes, then emits a
> labeled fit prediction without changing the wired limit. `temper update [id]`
> re-resolves existing rows through the shared pinning boundary and a single
> concurrency-safe lock commit, then prints (without running) the coder or
> reranker acceptance gate. Hermetic tests pin
> concrete manifest-field → config mappings, a full llama-swap golden,
> JSON/YAML validity, dry-run purity, mode switching, immutable generations,
> aggregate check findings, full-hash mismatch detection, exact budget
> arithmetic, unavailable/not-applicable budget states, machine-read fallback,
> interrupted-effect refusal, concurrent lock protection, and clean second
> runs. No test uses the network or touches the live service. The manually
> maintained 2026-08-19 coder+reranker fixture has also passed a real isolated
> `--dry-run`, first apply, clean second apply and semantic comparison with the
> running legacy configuration; reviewed differences are recorded in
> `docs/acceptance/current-posture-render.md`.

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
   tested-status is `check`'s comparison against the relevant signed catalog,
   never a lock field.
2. *(design)* Placement: beside the manifest in the current layout. The
   proposed `~/.temper` home (D3) moves manifest, lock, and state together
   if adopted — the schema must not care where it lives.

Then the verbs:

3. *(build — first slice complete)* Native `apply`: strict manifest + lock →
   verify selected immutable sets → pure render → immutable generation → one
   atomic pointer commit. Generated model and patch paths are exact,
   lock-derived local paths; v1 text-only layouts explicitly disable projector
   discovery.
4. *(build — complete)* `resolve` fills missing lock rows and never moves
   existing pins; `fetch <layout>` materializes one exact content-addressed
   artifact set. These are separate commits from `apply`: the lock beside the
   manifest and the rendered pointer under an explicit root cannot be changed
   atomically, and hiding two commit points in one verb would violate §1.
   Locking never forces a weight download. A later `start`/mode slice invokes
   the same fetch effect for absent artifacts rather than growing a second
   downloader.
5. *(build — complete)* `update [id]` — re-resolves upstream, prints old→new per
   entry, and ends by *printing*
   the targeted gate (coder: the streaming tool-call curl plus a plain
   completion; reranker: the magnitude probe) — never running it. From
   M2 it also warns when the new pin leaves the active catalog's tested set.
   Per-entry
   is the normal move; bare `update` exists but bundles unrelated risk and
   says so.
6. *(build — complete)* `check` is a read-only,
   offline aggregate report: whole-manifest lock drift, selected-mode artifact
   admission, explicit `--verify` full-byte hashing, and the pure wall-model
   calculation documented in `docs/design/wall-model.md`. The budget uses
   read-only machine facts plus sizes from admitted receipts and always labels
   its output a *prediction*. The advisory wizard-diff slice waits for M3's
   recommendation data.

**Acceptance:** M1 gates are hermetic and green: concrete render units plus
full golden, missing/malformed artifact refusal, `apply && apply` clean, and
`--dry-run` mutation-free.
`update` on a fixture moves exactly one row, preserves every non-target row,
and prints the right gate text; its dry-run, all-layout atomicity, second-run
cleanliness, and concurrent-writer refusal are hermetically covered. Budget
tests cover exact rounding, co-tenants, CPU placement, receipt-backed model
lower bounds, live/default wired limits, and CLI findings.
gofmt + go vet + table tests throughout.
**Dependencies:** none for the first slice. **Decisions:** D3 any time before
M3.

### M2 — software supply catalog + Field Kit execution base

**Goal:** a consenting Mac can resolve, install, identify, verify, and remove
the exact software base required by Labs-promoted Field Kit experiments; the
broader reviewed configuration catalog follows without blocking that execution
base.

> **Sequence changed by owner 2026-08-20.** Build the minimum software supply
> catalog first, immediately use it to install the Field Kit base, then expand
> the broader qualification catalog. Field Kit does not wait for the wizard,
> production modes, harness leases, or live-machine cutover.

> **Field Kit boundary revised by owner 2026-08-24.** The new Field Kit is a
> user-facing, agent-operated catalog of current machine-dependent experiments,
> not a friend-only installer. Labs authors experiments and promotes immutable,
> bounded packages into Field Kit. Its root prompt reads Temper's canonical
> machine facts, suggests applicable experiments with cost estimates, and asks
> the user to opt into named experiments. Per-experiment prompts may adapt
> within reviewed limits while Temper supplies stable mechanical execution and
> provenance. Experiment promotion is not product/profile promotion. See
> `docs/design/field-kit-experiment-boundary.md`.

> The software-supply catalog is **an independently published, signed database
> of tested software versions**; the qualification catalog separately records tested composed
> configurations. "Minimum tested" is evidence, while "latest version
> satisfying this floor" is update policy; neither is the exact installed
> version. Every actual installation is frozen in `software.lock.yaml` and
> witnessed by an installation receipt. Rolling policy is evaluated only by an
> explicit resolve/update—there is no background updater and no floating
> installation.

#### Phase A — software supply first

> **Surface approved 2026-08-20:** C4/C5, independent signed-catalog lifecycle,
> and the adapter-family protocol are settled in
> `docs/design/software-supply-schema.md`. The first hermetic
> schema/target-selection/digest slice may now consume them.

> **Shared resolver slice complete 2026-08-20:** strict catalog and
> software-lock parsers/validators, normalized target selection, the compiled
> keyed-adapter descriptor handshake, provider-neutral candidate closures,
> SemVer/PEP 440 policy selection backed by pinned maintained parsers, exact
> closure invariants, canonical semantic/root digests, and the
> dry-run/concurrency-safe atomic lock-writing
> transaction are executable and hermetically tested. The internal signed
> catalog-update slice now strictly verifies detached Ed25519 channel/catalog
> artifacts, their digest/schema/sequence join, complete compiled capabilities,
> rollback/equivocation policy, immutable storage, and the concurrency-safe
> active-pointer commit. The Homebrew candidate edge now translates its
> recursive formula closure and JSON v1 bottle metadata through an injected,
> total-budget command runner, refusing incomplete graphs, wrong target tags,
> and unhashed artifacts; all tests remain hermetic. Its production process
> edge now invokes no shell and forces Homebrew auto-update, analytics, prompts,
> and incidental GitHub API access off. The read-only catalog selector verifies
> either the active snapshot or an injected embedded fallback, and resolution
> returns the four derived tested-status states, including transitive
> exclusions. The bounded HTTPS catalog source now implements the signed
> publication transport convention. The first production signing key's public
> trust root and signed sequence-1 bootstrap are now compiled release inputs;
> the bootstrap contains only the reviewed llama-swap v251 and llama.cpp b10566
> release recipes and correctly carries no tested-version evidence. A pinned
> signature vector covers the trust root, and no private signing material enters
> the tree. The reviewed GitHub Pages channel root and the bounded public
> `temper software catalog update` command are now wired through the production
> trust/source/capability composition, with a 30-second deadline and hermetic
> command coverage. The signed stable channel and immutable Pages source tree
> are staged and join-tested under `docs/catalog`; Pages enablement/publication
> remains an explicit external step. A retained release-only `temper-catalog`
> command now accepts the private seed only on stdin, validates the compiled
> production trust/capability boundary, and atomically signs or verifies exact
> catalog and channel bytes with dry-run and clean-rerun coverage. The uv
> resolver is now executable behind injected, bounded process and HTTPS reads:
> it couples managed-Python metadata to the installed stable uv 0.12.x
> protocol, selects and hashes the exact target CPython build, translates
> wheel-only PEP 751 output, restores catalog dependency edges, and refuses
> markers, alternate sources, missing hashes, or protocol drift. Its tests are
> hermetic and the complete candidate passes the shared selector/software-lock
> invariants. This closes the Phase A product engineering slice on 2026-08-24;
> Pages publication remains a separate explicit release action. The selected
> release-artifact source, deterministic
> resolver, production HTTPS reader, and isolated install/inspect/remove member
> are now executable with hermetic archive/effect coverage. Exact macOS
> host-target detection and the frozen public install/check/remove commands are
> wired to that compiled member with a hermetic command-level round-trip; the
> real adapter scratch lifecycle also passes. The Homebrew edge
> is an available shared adapter, not a default application-installation
> method anywhere in Temper.

1. *(design)* Define typed logical-package, installation-method,
   target-adapter, and adapter-native package-recipe records (C4). Keep three
   levels explicit:

   - **method** is the portable strategy, such as `system-package`,
     `python-environment`, `release-artifact`, or `source-revision`;
   - **adapter** is the concrete target implementation, such as today's
     macOS `homebrew`, `uv`, or an upstream-release client. A future target may
     bind `system-package` to `apt`, `dnf`, `pacman`, `winget`, or another
     reviewed adapter without teaching the install workflow that operating
     system's commands;
   - **package recipe** binds a logical package to the adapter-native package
     name, version scheme, constraints, and commands/artifacts understood only
     by that adapter.

   A logical package and an installation method are different identities.
   The method record owns portable semantics; the target binding is the one home
   for canonical method + target → adapter selection; and the adapter record
   owns effect class (isolated/shared), capabilities, and adapter-protocol
   revision. Package recipes reference those records rather than repeating
   their facts. Target selection may choose only a catalog-declared adapter for
   that method and target, with at most one canonical adapter per method/target
   in one catalog snapshot; alternatives are explicit variants. The exact adapter is shown
   and locked. Changing method (`system-package` → `python-environment`) or
   choosing an undeclared adapter is an explicit choice, never a fallback.
   Package recipes declare the
   provider-native version scheme (SemVer, PEP 440, Git revision, or opaque),
   selection policy (`latest`, range, exact version, or revision), compatibility
   floor, direct/transitive constraints, exclusions, known-bad versions,
   recipe revision, verification gates, data/license boundary, and exact
   tested-version evidence with its source.

   Catalog publication applies a Temper-wide package-specific curation rule
   before any recipe ships: prefer an isolated verified release artifact for a
   native application and an isolated exact environment for a language
   application;
   admit `system-package` only for bootstrap/environment managers, genuine
   global dependencies, software available only there, or a demonstrably more
   maintainable distribution; build from source only as a last resort. This is
   release-review policy, not a resolver fallback chain.

   Keep the first product inventory finite and explicit. On macOS, Homebrew may
   own the shared bootstrap executables `uv` and `hf`; their exact desired and
   observed identities remain software-lock and receipt facts, and the `hf` CLI
   must not be confused with llama.cpp's forbidden moving `-hf` selector.
   `llama-swap` and `llama.cpp` are Temper-managed Field Kit base packages;
   the 2026-08-24 review selected isolated `release-artifact` installation for
   both on macOS Apple Silicon after static and bounded runtime gates.
   The concrete `upstream-release` adapter is hermetically executable and its
   real scratch lifecycle now passes; stable Results or Field Kit evidence is
   still required before either recipe can claim `exact-tested`.
   `rapid-mlx` and `mlx-dspark` are supported non-default
   `python-environment`/`uv` packages whose recipes constrain exact interpreter
   and MLX closure units under resolved D16. Pi is a user-managed harness,
   absent from the software-supply catalog. Manifest selection permits only
   integration rendering and
   compatibility reporting; it never authorizes installing, updating, or
   removing Pi, Node, or a JavaScript package manager.

   Existing schema tests may retain rolling, guarded-rolling, and constrained
   product-shaped fixtures to exercise policy and adapter behavior without
   inventing release versions. A fixture's method is not a reviewed seed-catalog
   decision.
2. *(design)* Define `software.lock.yaml` (C5) separately from
   `manifest.lock.yaml`. The manifest lock owns model/patch resolution; the
   software lock owns the exact executable environment and can exist before a
   user manifest. It records the selected method and concrete adapter, exact
   provider-native version/revision, resolved transitive closure, artifact
   location and hash where available, recipe revision, target OS/distribution/
   architecture facts used for selection, and resolution time. It does not
   duplicate tested evidence or claim what is installed.
3. *(design + build)* Implement resolution and installation methods as keyed
   **adapter families**, not provider/OS switch statements in CLI verbs. The
   family owns the adapter registry and refuses an unknown or declared-but-
   unbuilt key. Each concrete adapter translates vendor commands and output at
   its edge into Temper-owned plans, observations, outcomes, and receipt facts;
   package-manager types never escape into catalog or orchestration code.

   Resolution and installation remain separate functional units behind that
   boundary: a resolver adapter reads provider-native candidates; pure policy
   selects one; an installer adapter inspects and changes actual state. The Go
   composition root injects host facts and external command runners once. A
   new target adapter adds one family member plus its catalog target binding;
   the resolve/install/check/uninstall workflows do not change.
4. *(build — complete)* Implement the catalog validator and provider-native resolvers.
   Candidate reading is an explicit upstream read; selection from those
   candidates is a pure, deterministic computation. Resolution writes the
   complete candidate lock once and never installs. Existing locks move only
   through an explicit update. A method or adapter change is printed as such;
   target-based adapter selection is valid only when the catalog declares that
   exact binding. The strict validator, signed activation lifecycle, and
   provider-neutral resolution transaction are complete. The Homebrew
   candidate reader protocol and strict translator are complete behind an
   injected runner; its production non-shell process binding is complete. The
   uv surface now models `cpython` as a typed adapter-native runtime dependency:
   each Python application recipe constrains it and the resolved lock records
   the exact uv-managed interpreter artifact as a closure unit. The uv provider
   reader/translator now reads the installed stable uv 0.12.x protocol,
   version-matched managed-Python metadata, and wheel-only PEP 751 output
   through bounded injected edges. It returns one exact target-bound runtime/
   package closure and refuses any fact it cannot translate without weakening
   the lock.
5. *(build — complete)* Add the tested-status read: compare exact software-lock pins with
   the software-supply catalog and distinguish exact-tested,
   policy-eligible-but-untested, known-bad, and outside-policy states without
   storing a local verified flag. The pure comparison covers root and transitive
   exclusions, and software resolution returns the derived status for every
   requested package.

#### Phase B — Field Kit execution base immediately after

6. *(design, with Field Kit)* Freeze C10 and the Field Kit-facing parts of
   C11: exact commands, exit codes, stable RESULT lines, dry-run output, and
   Temper-material identity. Experiment discovery, applicability, consent,
   adaptive prompts, stage orchestration, and session reports stay in Field
   Kit; Temper provides canonical machine facts, exact software installation,
   isolated profile rendering, scoped service lifecycle, existing
   model-artifact verification, and provenance-guided removal. Labs alone
   promotes experiment packages into Field Kit under C12.

   The Temper-side surface is approved and concrete in
   `docs/contracts/software-install.md`: the `temper software install`
   invocation and output, named base/experiment installations, C5 experiment
   provenance and base requirements, C6 receipt/root state, prepared recovery,
   shared claims, and ordered packet identity are specified. C5 validation and
   the pure planner are executable. Strict canonical C6 documents and stores,
   plus internal keyed-adapter orchestration through prepared intent, observed
   receipt, and finalized claims, are now hermetically executable. The frozen
   read-only check surface is implemented as a pure drift analyzer plus a thin
   lock/store/adapter reader. The original Field Kit is only a behavior oracle
   for its replacement; Field Kit and Labs repository coordination remains a
   separate authorized cross-repository step.
7. *(design + build)* Install only from an already-resolved software lock and
   compute the complete plan plus pre-existing state before any effect.
   Every installation runs through the adapter family; CLI orchestration never
   invokes `brew`, `uv`, or any future package manager directly. Adapter
   capabilities and effect semantics are explicit:

   - isolated adapters (for example a `uv` environment or verified release
     artifact under the Temper root) stage, verify, gate, then atomically
     publish one target;
   - shared adapters (for example a system-package adapter operating in a
     machine-wide prefix) are enabled only with an ownership, pre-state,
     idempotency, and interrupted-run reconciliation contract. Temper never
     removes a pre-existing package, guesses ownership, silently changes
     method/adapter, or invokes `sudo`. A needed privilege step is printed as a
     ready-to-paste `[manual]` action.

   A failed isolated install leaves that named installation's published scope
   unchanged. One root-wide state commit records immutable operation intent and
   provisional shared claims before effects. A shared-adapter interruption may
   leave an inspectable external effect; the next run reconciles provider,
   receipt, and claim state before proceeding and records only what it proves.
8. *(build)* Write C6 only from observed post-install state. Each receipt binds
   installation ID, exact method/adapter/provider closure, hashes, verified
   base receipts, and locations to its software lock. Root state separately
   owns current shared acquisition/claims and prepared operations. Receipt is
   history; root state is removal/concurrency authority; neither is desired
   policy.
9. *(build)* Add read-only base check/status and provenance-guided uninstall.
   Check derives provider/lock/receipt/claim drift. Uninstall releases only the
   named installation's claims, removes a shared unit only after its last claim
   and only when state proves Temper acquired it, and preserves every
   pre-existing shared dependency. Isolated removal stays below that
   installation directory. Dry-run never mutates and every successful second
   run is clean.
10. *(build — complete)* Bind each Field Kit run's Temper material to
    canonical machine facts,
   the exact Temper binary checksum, an ordered set of named base/experiment
   software lock + receipt identities, manifest lock, and rendered-generation identity.
   Required base receipt identities are recursively explicit. The pure
   `temper-field-kit-binding/v1` builder now validates exact darwin/arm64 macOS
   machine facts, binary and manifest-lock byte identities, rendered-generation
   identity, and the caller-ordered lock/receipt set. Every requirement must
   identify an earlier supplied receipt and copies that receipt's complete
   recursive identity. The Field Kit-owned session envelope will additionally
   bind the promoted experiment version, metadata/prompt hashes, consent,
   attempts, decisions, observations, and report; Temper does not parse that
   moving envelope. For this pre-release slice, Field Kit may receive a
   checksummed darwin/arm64 Temper binary directly; choosing the final public
   Homebrew/curl/release channel remains M5/D4. Neither this root nor its
   services point at the live consumer home or legacy service.

11. *(design — complete 2026-08-24; cross-repository build pending)* Freeze
    the ownership and minimum promotion semantics of C12 in
    `docs/design/field-kit-experiment-boundary.md`. Labs is the single editable
    home for experiment definitions; a Field Kit package is an immutable
    promoted snapshot. Promotion must cover hard applicability and versioned
    buckets, advisory relevance, cost and data boundaries, consent, bounded
    adaptivity, evidence/provenance, cleanup, hermetic validation, and
    invalidation/retirement. It certifies a safe and useful experiment, not a
    positive hypothesis or product recommendation. Replacing the current Bash
    Field Kit waits for fixed and adaptive parity over Temper, then requires an
    explicit adjacent-repository step.

#### Phase C — broader qualification catalog resumes

12. *(decide, D1; design — provisionally approved by owner 2026-08-25;
    refinement remains open before v1 freeze)* Define C7 as separate typed
    profile documents over
    a common envelope for model artifact, engine, model runtime, tool, mode,
    and activity. The envelope carries exact pins, status, witness scope key
    (artifact revision × engine-profile revision × runtime-profile revision ×
    machine bucket × mode × co-residents), dependencies, known failures, data
    boundary, invalidation triggers, applicability, roles, and a "what this
    means for you" line. Applicability says where a profile is useful; witness
    scope says where evidence exists. Runtime profiles remain plural per
    artifact because fit, stability, cache, and performance evidence may vary.
    Recommendation is a plural, consent-neutral projection over qualified and
    applicable rows, not a status and not a ranking: zero, one or many layouts
    may be recommended for the same machine/mode/role. Each model-runtime row
    carries a structured performance profile covering task success/regressions,
    time-to-solution/tool use, raw throughput, qualified context, memory and
    cache behavior with conditions and unmeasured axes explicit. The schema
    must distinguish catalog recommendation from the manifest's user-owned
    selection and `preferred` member flag.
    The provisionally approved contract is
    [`docs/design/qualification-catalog-schema.md`](design/qualification-catalog-schema.md):
    six content-addressed typed profile documents, immutable supersession and
    status history, separately versioned machine-bucket vocabulary, and
    unordered catalog-level recommendation sets. It proposes the D7 rule that
    changed material returns through `LAB`, while deliberately parallel
    supported combinations use separate profile IDs.
    Temper refinement currently implements the exact-reference and index
    surface plus strict bucket-only bundle loading: canonical index bytes,
    derived paths, hashes, and bucket identities are verified from a supplied
    in-memory bundle. Nonempty profile and recommendation content is explicitly
    refused until its typed validators exist. The fixtures remain fake and do
    not seed a qualification row.
13. *(design, with Labs — Temper side provisionally approved by owner
    2026-08-25; Labs adoption still requires explicit authorization)* Define
    the product-promotion packet
    (C8): how a reviewed Labs packet compiles into a qualification row without
    becoming consent. The provisionally approved Temper-side contract is
    [`docs/design/product-promotion-contract.md`](design/product-promotion-contract.md):
    one canonical packet targets one C7 revision, private/raw provenance stays
    in Labs, and a pure compiler emits only public-safe claim-level evidence
    plus exact packet identity.
    `field-kit-runtime-profile/v1` is the exploratory-witness special case;
    `external-lab` packets stay inspectable but outside the generic install
    path.
14. *(build)* Extend validation with status-machine legality, witness-scope
    completeness, applicability, and consent-neutrality (no row selects
    itself). A binding to an unselected item, or an unqualified combination
    presented as qualified, fails rendering.
15. *(build)* After accepted C8 packets exist, seed the reviewed current
    posture as narrowly scoped `QUALIFIED` rows and compile a native-MTP
    candidate as a consent-neutral `LAB` row. It remains opt-in, non-default,
    and non-recommended, with any rejected autonomous M5/32 GiB scope retained
    exactly. As checked on 2026-08-24, the current Labs handoff registry
    contains no accepted native-MTP C8 packet; legacy evidence or a Field Kit
    runtime-profile packet is
    inspectable input to future Labs review, not an accepted fixture and not
    permission to read moving Labs state or alter the live legacy service.

**Acceptance:** Phase A fixtures cover all three policies, provider-native and
non-SemVer comparison, exact method/adapter/closure locking, known-bad
exclusions, refusal of silent method fallback, deterministic target→adapter
selection, and an unknown-adapter refusal. The same adapter-contract suite runs
against every member; adding a fake second-OS system-package adapter requires
no workflow change. The reviewed initial inventory contains no Pi/Node recipe,
and an explicitly selected harness remains render/check-only. C5 fixtures also
cover direct experiment provenance, combined catalog/experiment provenance,
and canonical required-base digests. Phase B uses hermetic fake adapters to
prove dry-run purity, clean second runs, concurrent-run refusal, interruption
reconciliation, named-root isolation, two experiments claiming one exact
shared package without reinstall/removal races, base-receipt drift refusal,
preservation of pre-existing packages, exact uninstall, and ordered packet identity. A
real scratch round-trip through one promoted fixed experiment and one promoted
bounded-adaptive experiment is on-demand, announced, and run only with explicit
authorization. The current Bash Field Kit does not satisfy this replacement
gate merely by integrating the Temper binary. Phase C round-trips a fake packet into a
wizard-readable row and rejects every illegal fixture.

**Dependencies:** M1 for phases A and B. Phase C is required by M3. Labs-side
parity (`add-tool` intake) is tracked in Labs and does not block the installed
base. **Decisions:** D1 and D7 are provisionally accepted for Temper-only
fake-fixture implementation and remain refinable before v1 freezes. D14 fixes
the Phase A method/adapter boundary. D4 does not block a checksummed pre-release
Field Kit binary.

### M3 — wizard

**Goal:** the choice surface — the spec's seven steps, advisory on re-run.

1. *(build)* The TUI foundation: bubbletea/huh over the catalog reader
   (D2 resolved — §4). Distribution (D4) is worth deciding by here so the
   toolchain and release shape stop being hypothetical.
2. *(build)* The flow per spec, **reordered modes-first 2026-08-19**:
   detect hardware/allowances and choose the modes → one screen per chosen
   mode (its models, its tools one-by-one with backend, data-boundary and
   dependency consequences shown before selection, its harnesses, and
   keep-loaded per model) → preview (downloads, disk, memory, mode
   transitions, external data paths) → write `manifest.yaml` once, render,
   probe. Tools start unselected; recommendations are explanations, never
   checked boxes.

   Build notes that follow from the reorder, all specified in SPEC's
   "Screen 1 / Screen 2" subsections: the mode list is catalog-derived (a
   mode is offered when the catalog can furnish it on this machine) and an
   unfurnishable mode renders *disabled with its reason* rather than
   hidden; the per-profile applicability envelope (M2) clips each screen's
   offers; consent is per tool but exposure is per mode, so a tool's first
   appearance is the full consequences question and later ones are a plain
   checkbox; harness *detection* is global while *enabling* is per mode;
   and the download figure is a union across modes, so per-screen totals
   must be labelled "this mode" or the preview total will not match what a
   user adds up. A model screen shows every applicable recommended layout in
   its comparison group, explains the material performance-profile tradeoffs,
   and leaves every checkbox and preferred-model radio to explicit user input;
   it never picks a catalog “winner.”
3. *(build)* Advisory re-run: manifest present → print divergence from the
   current bucket recommendation, stop. No interactive stage under
   `--dry-run`, no second-run mutation — "written once" buys both.
4. *(build)* The wizard preview invokes the same native pure renderer as
   `apply`; there is no second template path. `code-organization`'s Go
   reference governs layout (`cmd/` composition root, `internal/` packages);
   wiring lives at the root, families construct their own members.

**Acceptance:** the wizard on a fixture machine profile produces a manifest
that `apply` renders byte-identically to its hand-written equivalent; a
consent audit finds no path from recommendation to selection without an
explicit choice; a fixture with two co-recommended coder layouts displays both
tradeoff profiles, permits installing either or both, and chooses neither by
default; re-run is advisory only.
**Dependencies:** M2 Phase C (the qualification catalog is what it browses),
M1.
**Decisions:** D3 (where the wizard writes), D8 (remote providers
render-only), D4 (latest useful point).

### M4 — production mode state machine + harness qualification

**Boundary (reordered 2026-08-20).** The reversible Field Kit base lands in
M2 Phase B. M4 does not create a second installer or make Field Kit wait for
production mode machinery. Probes remain Field Kit's; this milestone adds the
consumer-facing mode state machine, advisory leases, and qualified harness
cooperation over the already installed and receipted base.

**Labs prerequisites** *(measure — experiments run in Labs, before any mode
binding ships as qualified)*:

- llama-swap config-reload under in-flight requests: does `--watch-config`
  unload a resident coder cleanly, or does the switch need an explicit
  unload call? Measure, don't assume.
- mode-switch latency and warmup cost, per the render diff's prediction.
- role-alias mechanics (harnesses speak `rerank`; the mode binds it).
- lease semantics under two live cooperating harnesses.

Work items:

1. *(design + build)* Mode machinery: `mode <name>` (render target → compare
   current state → lease check → commit swap → kick; reports loads/unloads and
   warmup cost);
   `start`/`stop`/`status` as the off-mode transitions of the same state
   machine. `mode --request <name>` is the harness-facing form: it
   succeeds only lease-free and never preempts — the verb a harness calls
   when it notices its coder idle; `--force` stays human-only (item 2).
   `status` distinguishes job loaded / process alive / answering
   with residency; `stop` prints the wired memory it freed. The target render
   remains the M1 pure function of `(manifest, mode, machine)`; no incremental
   delta engine or second residency source of truth is introduced.
2. *(build)* Leases (C9): an advisory state file (harness, mode, expiry),
   renewed while active; `temper mode` honors live leases; `--force` stays
   human-only. Idle detection lives in harnesses — no watcher here, the
   no-daemon rule holds. The harness side of the cooperation — renewing
   the lease while active, calling `mode --request` on idle or on a
   coder-model switch (Pi's case) — ships with each harness adapter, and
   its protocol test is part of the two-live-harnesses prerequisite above.
3. *(design + build)* Ship harness adapters as clients of the mode/lease
   protocol. Roles remain the stable harness interface and each selected mode
   owns the role→runtime binding; a missing required role is invalid or visibly
   unavailable, never silently substituted. Adapter packaging remains D10.
4. *(build)* Add the consumer status snapshot (`report`) and reconcile
   production service state without absorbing Field Kit packet/report
   generation. Re-witnessing after a software or model update remains a Field
   Kit run against the M2 base.
5. *(measure + release review)* Run the prerequisite experiments and protocol
   soaks, then add only the witnessed machine/mode/harness bindings to C7.

**Acceptance:** a mode switch is witnessed under in-flight load; two live
cooperating harnesses obey a lease without preemption; `local`, `utility`, and
`off` transitions are second-run-clean and preserve exact active-state
identity; unqualified bindings remain unavailable. The M2 Field Kit base tests
remain a prerequisite suite, not duplicated here.
**Dependencies:** M2 Phase C, M3, plus the Labs prerequisites above.
**Decisions:** D6 (lease semantics), D10 (adapter distribution channels),
D11 (which modes ship qualified). D5 moved to the field-kit/Labs side —
see the register.

### M5 — release

**Goal:** `temper-sh/temper` public, the live machine cut over, the org
story complete.

1. *(gate)* Cutover: Temper reaches parity or has a reviewed improvement for
   every part of this machine's posture — the comparison to legacy records
   intentional differences, a Field Kit run on the Temper-installed base
   passes, and the acceptance suite is green. Then the live box moves to
   Temper as its first witness machine and legacy `setup.sh` freezes.
   Owner-scheduled; announced; reversible until the freeze.
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

- **The native Go renderer starts with M1.** It consumes the native manifest
  and lock directly; `apply` and the renderer are one product path from the
  first executable slice.
- **Legacy Bash remains external evidence.** Its extraction makes comparisons
  cheap, but it is not copied, exec'd, or supported as a compatibility layer.
  A comparison may require the same behavior or document why native output is
  better; byte identity is not a goal across different schemas.
- **The lasting regression fence is native.** Small table tests pin individual
  schema facts to output content, full goldens pin artifact shape, shared-set
  tests pin canonical receipt and filesystem admission, and apply tests pin
  refusal, dry-run, immutable generations, and second-run cleanliness.
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
- package moves, public operation paths, decision/effect ownership, and
  durable state layout update `docs/CODE.md` in the same change; narrow
  implementation details stay in package comments and black-box tests.
- each named phase/milestone closeout through M5/1.0 includes its brief
  real-work craft field note; the note may correctly propose no change and
  never authorizes an eval or product-side scope expansion.

Go (the CLI — §4): gofmt + go vet + table tests plus native config goldens.
Legacy comparison is an explicit cutover review aid, not a permanent runtime
or CI dependency.

## 6. Decision register (owner)

| # | Decision | Blocks | Current lean |
|---|---|---|---|
| D1 | Qualification-catalog representation: typed documents vs normalized graph | M2 Phase C | **provisionally approved 2026-08-25:** six content-addressed typed documents plus catalog-level bucket/recommendation vocabulary; refine before v1 freeze (`docs/design/qualification-catalog-schema.md`) |
| D2 | Language | — | **resolved 2026-08-14: the whole CLI is Go; completed 2026-08-19 for the first `apply` slice by starting native rendering in M1** (§4) |
| D3 | Adopt `~/.temper` as the machine-identity home | M3 (wizard write location); M1 schemas stay location-neutral; M2 Field Kit work uses an explicit isolated root | spec proposes yes |
| D4 | Final public distribution: brew vs curl-installer vs release asset (including prebuilt darwin/arm64 vs build-at-setup) | M5; does not block the checksummed pre-release Field Kit binary in M2 | open |
| D5 | Mode-posture soaks as Labs-promoted Field Kit experiment packages (Temper only guarantees the base can render and serve the requested posture in isolation) | M4 qualification | open — a Field Kit/Labs question (spec Q8) |
| D6 | Advisory lease file with expiry sufficient; `--force` human-only | M4 | leaning yes on both (spec Q7) |
| D7 | Witnessed-row versioning on engine update: invalidate vs fork | M2 Phase C schema design | **provisionally approved 2026-08-25:** immutable old witness; same product lineage supersedes through a new `LAB` revision, while deliberately parallel support gets a new profile ID; refine before v1 freeze (`docs/design/qualification-catalog-schema.md`) |
| D8 | Remote-provider integration strictly render-only | M3 | leaning yes (spec Q4) |
| D9 | Qualification-catalog contribution flow for Field Kit witnesses | post-M5 | Labs review and explicit product promotion; submission transport remains open |
| D10 | Pi `packages` / Codex / Claude Code plugin packaging as distribution channels | M4 adapters | open (spec Q9) |
| D11 | Which modes ship as qualified v1 | M4/M5 | **narrowed 2026-08-19**: the four candidates collapse to `local` + `utility` (+ `off`) — a mode is who owns the foreground model; tool narrowing is an activity, not a world. Still evidence-driven (spec Q2) |
| D12 | Development locus stance (§0: legacy remains the live reference, native product work is here, live cutover at M5) | — | **resolved 2026-08-19:** extracted Bash stays legacy-side; no compatibility/runtime landing; M1 starts native here |
| D13 | Who records `witness: verified` — a small `attest` verb vs some other mechanism; `check` must stay a pure read | M1 verbs | **closed 2026-08-18**: nobody — no local verified state at all; signed catalog snapshots carry tested-version evidence and `check` derives status by comparison |
| D14 | Installation portability boundary: hard-coded providers vs portable methods with target adapters | M2 Phase A | **resolved 2026-08-20 (owner):** every method is a keyed adapter family; `system-package` is portable intent, Homebrew is only the current macOS adapter, and the exact target adapter is catalog-declared and locked |
| D15 | Must one applicable model layout win the recommendation, or can several qualified tradeoffs be co-recommended? | M2 Phase C / M3 | **resolved 2026-08-20 (owner): recommendation is a consent-neutral set, not a ranking; several layouts may be recommended with distinct performance profiles, while selection and preference remain explicit user choices** |
| D16 | Where the uv adapter's Python implementation/version/ABI selection lives | M2 Phase A uv resolver; non-default `rapid-mlx` and `mlx-dspark` | **resolved 2026-08-20 (owner): Python is an adapter-native logical dependency (`python-runtime`/`cpython`) constrained by each application recipe; uv selects an exact managed interpreter and records its version, build revision, immutable artifact, and target-bound closure identity in the software lock. Ambient Python and machine target facts never supply it** |
| D17 | Whether Temper owns Node and harness executables | future Node-based managed package only | **resolved for current scope 2026-08-20 (owner): no Node adapter or runtime now; Pi and other harness executables are user-managed, while Temper may render/check an explicitly selected integration. Preserve the generic adapter boundary and revisit only for a concrete Temper-managed Node package** |
| D18 | Ownership of shared environment/model-source tools | M2 Phase A/Phase B bootstrap and receipt | **resolved 2026-08-20 (owner): on macOS, Homebrew may install and own the shared `uv` and `hf` executables. uv owns exact isolated Python runtimes and application closures below that layer. Artifact downloads through `hf` must use locked revisions; llama.cpp's moving `-hf` selector remains forbidden. The catalog supplies policy, the software lock supplies exact desired tool identities, and the receipt supplies observed installed identities instead of Temper accepting ambient PATH state** |
| D19 | Experiment-specific and run-time-generated software locks; ownership when installations share provider packages | M2 Phase A/B, Field Kit/Labs consumers | **resolved 2026-08-21 (owner): a lock records independent immutable catalog and experiment provenance and may require exact base-lock receipts; installation consumes the frozen lock without a catalog read. One explicit root holds many named base/experiment installations. Per-installation receipts are history; one root-wide state document atomically owns prepared intent and reference-aware shared claims, so one experiment cannot remove a package another still uses. Field Kit's Temper-material binding carries the ordered installation lock/receipt set** |
| D20 | Use Temper as the craft skills' first real-work canary through 1.0 | no product milestone; phase closeout documentation only | **resolved 2026-08-21 (owner): record an evidence-linked skill field note after M1 and each M2 phase/M3/M4/M5; propose narrow improvements or no change; never turn the secondary objective into synthetic product work or an ungated model eval** |
| D21 | Field Kit's replacement role and experiment ownership | M2 Phase B boundary; Labs/Field Kit rebuild | **resolved 2026-08-24 (owner): Field Kit is a user-facing, agent-operated catalog of current machine-dependent experiments. Labs is the editable source and promotes immutable, bounded experiment packages; the Field Kit root prompt uses Temper machine facts to suggest applicable experiments with costs and obtains per-experiment consent; experiment prompts may adapt only inside reviewed bounds. Temper supplies mechanics and provenance but never consumes the moving experiment catalog. Experiment promotion and product/profile promotion are separate gates; the original Bash implementation retires after parity** |

## 7. Now / next

1. **M2 Phase A — software supply (engineering complete 2026-08-24):** C4,
   `software.lock.yaml`, and the signed
   catalog lifecycle are approved; the shared resolver and authenticated
   catalog-store transactions are hermetically executable. Homebrew candidate
   translation, its controlled production process runner, authenticated
   active-or-bootstrap catalog reading, four-way tested-status reporting, and
   the bounded HTTPS publication source are complete. The production public
   trust root, signed two-package bootstrap, reviewed GitHub Pages channel root,
   and bounded public catalog-update command are wired and hermetically covered.
   The initial Pages publication tree is signed and locally ready; enabling and
   publishing it remains an explicit external action. Its signing lifecycle is
   retained in the release-only `temper-catalog` command rather than recreated
   per publication. The bounded uv 0.12.x reader/resolver now locks the exact
   uv-managed CPython build and PyPI wheel closure for explicitly requested,
   non-default Python packages; its process environment, version-matched
   metadata transport, PEP 751 translation, policy edges, and failure
   boundaries are hermetically covered. The 2026-08-24
   method review rejects both Homebrew application variants at the
   exact-install gate. The isolated `llama-swap` v251 and `llama.cpp` b10566
   artifacts passed the bounded model-backed runtime/router screen, selecting
   `release-artifact` with exact catalog-reviewed releases. Its strict
   `release-archive` source, deterministic `upstream-release` resolver,
   bounded HTTPS edge, and isolated install/inspect/remove implementation
   are complete with hermetic failure-boundary and round-trip coverage. The
   frozen C11 software verbs and exact host-target check are now wired to this
   compiled member and refuse any uncompiled locked adapter without fallback.
   The separately authorized real scratch install/check/remove/second-run gate
   now passes through that complete workflow, including both dry-run
   boundaries. No `exact-tested` row is published yet. The Field Kit-facing
   C10/C11 install surface is frozen. The signed Pages tree still requires the
   owner's explicit publication action; no code path publishes it implicitly.
2. **M2 Phase B — Field Kit execution base:** C10/C11 and Temper's half of the
   execution binding are implemented; C12's ownership and promotion boundary
   is now frozen in `docs/design/field-kit-experiment-boundary.md`. Build the
   new Labs-promoted Field Kit catalog/root prompt over those surfaces, then
   run explicitly authorized fixed and bounded-adaptive scratch round-trips
   with a checksummed Temper binary before retiring the current Bash kit.
   The Temper-side C5/C6/C10/C11 surface is approved. Generic lock validation
   and the pure planner now cover direct/catalog-backed experiment provenance,
   base-receipt requirements, named isolated roots, prepared recovery, and
   root-wide shared claims. Canonical receipt/root-state stores and keyed
   fake-adapter install orchestration now prove dry-run purity, clean second
   runs, live/expired operation recovery, base-receipt drift refusal,
   pre-existing preservation, and shared claims without reinstall. The
   read-only check/status path is now
   executable and reports missing/drifted provider state, missing/drifted
   receipts, required-base drift, unclaimed/shared-claim drift, and prepared
   operations without mutation. Provenance-guided uninstall is now executable
   behind keyed fake adapters: it conditionally releases receipts and claims,
   serializes the final Temper-added generation through `retiring`, preserves
   pre-existing and still-claimed units, refuses drift, and recovers explicit
   reruns without repeating a completed provider effect. Canonical
   `temper-machine-facts/v1` detection and the pure
   `temper-field-kit-binding/v1` schema/builder now bind exact binary,
   manifest-lock, generation, and ordered recursively explicit installation
   identities without reading or writing Field Kit state. The selected
   release-artifact effect member and frozen C11 public software verbs are now
   integrated through exact host detection and hermetic command-level
   install/check/remove/second-run coverage. The announced and authorized real
   adapter scratch round-trip now passes with the exact reviewed v251 and
   b10566 assets. Remaining work is cross-repository: Labs promotion rules and
   immutable experiment packages, the new Field Kit discovery/consent prompt
   and session envelope, followed by the parity and scratch gates. Temper must
   not integrate the old Field Kit merely to declare Phase B complete.
3. **M2 Phase C — qualification catalog:** the C7 typed-document and
   Temper-side C8 product-promotion surfaces are provisionally approved.
   The first C7 executable slice now strictly parses, canonically encodes,
   hashes, validates, and matches immutable machine-bucket documents against
   canonical Temper machine facts using a fake hermetic fixture. Its exact
   catalog index now canonically validates references and release paths, then
   verifies bucket bytes, hashes, and identities through a pure bundle loader;
   profile and recommendation loading fails closed. Continue C7 with the first
   typed profile document and its fake fixture while refining the surface.
   Co-design/adopt the C8 writer side in Labs only under explicit
   cross-repository authorization; its pure packet compiler follows that gate.
   Then add the plural recommendation/performance projection and reviewed seed
   rows. The first two-layout fixture should preserve co-recommended
   speed/context and quality-first coder layouts rather than manufacture one
   global winner. No real native-MTP row exists until Labs supplies an accepted
   C8 packet.
4. **M1 — complete and accepted locally:** keep the dated current-posture
   manifest/lock fixture and its field-to-config acceptance test current while
   this path remains isolated from the running service; the wall-model contract
   and implementation are in `docs/design/wall-model.md` and `internal/budget`.
5. **Labs:** queue the M4 prerequisite experiments (config-reload under
   in-flight load, mode-switch latency) through the new-experiment workflow
   when convenient — they gate production mode qualification, not the M2
   Field Kit base.
6. **Craft field evidence:** keep `docs/craft-skill-field-notes.md` current at
   each named phase closeout. The M1/current-M2 baseline and formal M2 Phase A
   closeout are recorded; the next formal note closes M2 Phase B.
