# Craft skills — real-work field notes

Temper is the craft set's first sustained real-product test. These notes close
the execution plan's craft-skill field-test objective: after each named
workstream through the 1.0 release, record how the skills affected actual work
and propose a narrow
improvement only when the phase supplies evidence for it.

This is not a model-eval log, product evidence, or permission to change the
skills repository. It creates no synthetic Temper work. A note may — and often
should — conclude that no skill change is warranted.

## Closeout format

Each note records:

1. phase, date, and the product artifacts or decisions reviewed;
2. skills that materially influenced the work — omit incidental loads;
3. guidance that helped and where it changed the result;
4. missing, misleading, over-specific, or costly guidance, including a defect
   or near miss when one exists;
5. proposed improvement and owning skill/seam, or **no change warranted**;
6. disposition: proposed to the skills plan, accepted, declined, or awaiting
   more real-world evidence.

Keep product-specific facts here. Any proposal transferred to the skills plan
must state the context-independent invariant and preserve the skills
repository's routing, stance, and eval gates.

## Baseline — native manifest complete plus installed-base work through 2026-08-21

**Status:** retrospective native-manifest closeout and provisional
installed-base observation. This does not close the software-supply or Field
Kit execution-base work; each still receives its own note.

**Artifacts reviewed:** native manifest/lock/render/check contracts and tests;
software-supply catalog, software lock, adapter family, signed-catalog
lifecycle, tested-status read, installation contract, and pure install planner.

### What helped

- **Data modeling:** surface-first and one-home-per-fact kept user intent,
  resolution, tested evidence, observed installation, and removal authority in
  separate manifest, lock, catalog, receipt, and root-state artifacts. That
  separation prevented the lock from claiming an installation and the receipt
  from becoming update policy.
- **Code organization and unit design:** the keyed adapter family gave
  `system-package` a portable strategy while keeping Homebrew and uv vendor
  details at concrete edges. Resolver reads, pure selection, inspection, and
  installation effects remained separate instead of growing OS/package-manager
  conditionals in CLI verbs.
- **Reliable effects:** staged validation, one commit point, explicit unknown
  outcomes, and reconciliation shaped both catalog activation and software
  installation. They prevented a package-manager effect from being mistaken
  for an atomic local transaction.
- **Testing:** test-by-unit-kind produced pure policy/planner tables, focused
  adapter contract suites, and hermetic effect orchestration. The result tests
  product outcomes without invoking the network, Homebrew, uv, or the live
  service.

### What the work exposed

1. **Provenance needed a smaller trust grain.** A software lock may combine
   independently authorized catalog, experiment, and base identities. A
   top-level source list says who participated but cannot justify each
   independently trusted decision.
2. **Shared-resource removal needed stronger authority.** Per-installation
   receipts are history, not permission to remove a shared package. Safe
   uninstall needs root-wide acquisition provenance, prepared/active claims,
   exact identity, serialized final retirement, and preservation when proof is
   absent.
3. **Effect testing needed named crash boundaries.** Happy-path plus generic
   failure tests do not cover the durable worlds between prepared intent,
   provider effect, observed poststate, receipt publication, and claim
   finalization.
4. **Long-lived contract evolution is a distinct question.** Catalogs may
   change more often than the binary while locks and receipts must remain
   interpretable. Schema labels alone do not answer reader/writer coexistence,
   canonical identity, absent/null semantics, or removal of old forms.
5. **Unit-level lifecycle remains under-owned.** The reliable shared-resource
   protocol now has a home, but a unit or library surface still needs a clear
   owner-versus-borrower rule for starting, stopping, closing, cancellation,
   retries, logging, and global configuration.

### Proposals and disposition

- **Accepted into existing craft guidance:** provenance at the independently
  trusted grain (`data-modeling`); destructive/shared-resource authority
  (`reliable-effects`); failure-boundary restart tests (`testing`).
- **Planned:** deterministic craft-set verifier; `contract-evolution`; the
  `unit-design` half of resource/lifecycle ownership after another phase note
  confirms the seam.
- **No change warranted:** no Temper evidence currently supports changing
  routing descriptions, adding a craft meta-skill, or expanding functional or
  language coverage. Those remain governed by their existing set-level
  evidence gates.

No model eval was needed to reach these observations. They came from product
contracts, implementation seams, and concrete failure windows encountered in
the phase work.

## Software supply complete, 2026-08-24

**Status:** formal software-supply engineering closeout. Enabling and publishing the
already signed Pages source remains an explicit release operation, not an
unfinished product-code effect.

**Artifacts reviewed:** typed software-supply catalog and exact software lock;
shared selection and atomic resolution; authenticated catalog activation and
retained release signer; Homebrew, upstream-release, and uv resolver edges;
the isolated upstream-release installation member; tested-status derivation;
and the hermetic command/effect suites that exercise those boundaries.

### What helped

- **Code organization and unit design:** provider reads, pure translation and
  selection, and installation effects remained distinct units. The uv work
  added one process reader, one HTTPS reader, and pure Python-metadata/PEP 751
  translators without changing the shared resolver or lock writer.
- **Data modeling:** the interpreter is an exact adapter-native closure unit,
  not an ambient machine fact. Keeping policy edges in the catalog and exact
  artifacts in the lock made it possible to consume uv's flattened PEP 751
  install set without pretending that it supplied dependency metadata it does
  not contain.
- **Reliable effects:** signed publication stages and validates complete bytes
  before one commit, and the retained signer makes key input an explicit
  release boundary rather than a temporary code edit. The upstream-release
  installer likewise publishes one validated generation and reconciles from
  inspectable state.
- **Testing:** every external protocol has an injected hermetic edge with
  bounded output, cancellation, no hidden retry, protocol-drift refusals, and
  end-to-end validation through the shared lock invariants. Real scratch work
  remained an announced separate gate.

### What the work exposed

1. **Stable format does not imply sufficient semantics.** PEP 751 is a stable
   artifact format, but uv 0.12 intentionally emits a flattened install set.
   An adapter must state what information was absent and use an owned,
   conservative projection rather than inferring a richer graph.
2. **Upstream protocol versions are one compatibility unit.** uv's executable
   version, version-tagged managed-Python metadata, command flags, and emitted
   pylock shape must be reviewed together. Accepting a new version while
   validating only one of those surfaces would create a false compatibility
   claim.
3. **Release secrets need a permanent narrow interface.** Recreating signing
   code for each publication obscures review and increases key-handling risk.
   A retained stdin-only command makes validation, dry-run, and clean reruns
   part of the product's release machinery without storing private material.

### Proposals and disposition

- **Awaiting more evidence:** carry the first two observations into the
  already planned `contract-evolution` work as candidate examples of a
  version-coupled upstream protocol and an explicitly lossy adapter
  projection. Software-supply completion alone does not justify changing that
  guidance yet.
- **No additional craft change warranted:** the existing organization,
  modeling, effect, and testing guidance covered the signer and all three
  adapter shapes without a new routing rule or skill. Reassess lifecycle
  ownership and cross-repository contract testing at the Field Kit
  execution-base closeout.

No fine-tuning or model evaluation was relevant to this phase. The engineering
decisions followed from inspectable provider protocols, typed contracts, and
failure-boundary tests.

## Embedded Field Kit runtime closeout (2026-08-27)

### Applied guidance

- **Code organization and unit design:** the ownership decision became one
  vertical `fieldkit` slice inside Temper: pure catalog/session/planning units,
  a command orchestrator, and narrow protocol/process/resource effects. The
  adjacent Field Kit repository now contains only promotion content.
- **Data modeling:** package revision 2 names an exact Temper protocol identity
  instead of a script path. The consented session binds catalog, package,
  material, machine, executing binary, software lock, outcome, stage evidence,
  protocol report, and final report without a moving runtime dependency.
- **Reliable effects:** start materializes immutable embedded bytes into a new
  owned root before one atomic external session commit. Each stage validates
  all consented inputs, records failed output without advancing, and commits a
  successful transition once. Restore remains marker- and confirmation-gated.
- **Testing:** hermetic workflow tests cover keep/restore, consent refusal,
  resumability, protocol dispatch, and embedded package validation. Protocol
  unit tests cover exact identity, structured-only evidence, resource stops,
  and refusal before effects. Live model/download work remains separately
  authorized.

### What the work exposed

1. A content repository and its runtime do not need the same distribution
   boundary. Embedding reviewed content in the effect-owning binary removes a
   second build and a second executable identity without collapsing Labs,
   promotion, and product-review authority.
2. Runtime behavior needs a versioned identity of its own. Replacing a bound
   script with `{id, revision, schema}` lets the package remain declarative and
   makes unsupported behavior fail at catalog verification.
3. Consent is stronger when it binds materialized bytes. Copying the exact
   embedded package and machine facts into the dedicated root makes every
   resumed stage check the same local evidence rather than trust a checkout.

### Proposals and disposition

- Add embedded/source byte-parity as a permanent release check when the release
  workflow is formalized.
- No craft-skill change is warranted yet; the existing vertical-slice,
  explicit-effect, canonical-modeling, and hermetic-test guidance directly
  covered the ownership refactor.

No fine-tuning or model evaluation was relevant. The problem was distribution,
authority, and effect ownership rather than learned model behavior.

## Additive V3 execution-lock slice (2026-09-13)

Data modeling kept portable compatibility, exact release material and observed
machine facts separate. The real Qwen install/bind/probe witness exposed a
remaining exact-host comparison in the binding reader; a contract test now
covers compatible locks across observed macOS versions while preserving legacy
exact-host refusal. Code organization and unit design kept compilation pure in
`internal/catalog`, publication in the command layer, and runtime effects in
their existing owners. Reliable effects guided non-replacing file publication
and exact partial-export repair; testing covered those outcomes before the
isolated runtime witness. Field Kit consumes a public export contract rather
than copying the graph compiler. No craft-skill change is warranted from this
single slice; the compatibility reader incident is the concrete seam to watch.

## Direct runtime release closeout (2026-09-22)

The first native alpha.8 check exposed a false lifecycle assumption in the
hermetic helper: real llama-swap starts an engine group of its own and uses a
basename for argv[0]. The helper now reproduces both behaviors. Temper reads the
kernel executable path, binds each group's identities, and proves every group
absent before cleanup. Field Kit measures those identities without owning their
shutdown. The corrected alpha.9 candidate passed the bounded native check; the
failed attempt and refusal were retained during diagnosis. Existing reliable
effects and testing guidance covered the correction; no craft change is needed.

## Catalog distribution (2026-09-22)

The current catalog reuses the existing signing trust and bounded HTTP reader.
Its signed channel owns publication sequence; authored model records do not
gain release bookkeeping. Explicit rollback retains the highest accepted
publication, so local recovery does not weaken network downgrade refusal.
One atomic state commit and a kernel writer lock cover update/rollback races;
the tests exercise interrupted staging, offline use and preserved user files.
Latest compilation retains the exact authenticated source digest while resolving
new software inputs. These are applications of the existing organization,
data-modeling, reliable-effects and testing guidance; no skill change is needed.

## Guided setup (2026-09-23)

The wizard reuses Selection and Execution Lock instead of adding a second
configuration schema. Pure planning, read-only catalog/model inspection, the
terminal state machine and filesystem publication have separate owners. An
accepted review binds exact bytes so a moving latest release cannot change
between review and preparation. Exclusive directory publication keeps multiple
mode selections and locks complete; resume preserves edits and credits existing
model sets when checking remaining disk space.

The native terminal check caught a review viewport that snapped back after
scrolling; a wrapped, small-terminal regression now covers it. Runtime review
also exposed the distinction between preparation, temporary supervised serving
and persistent helper availability. The wizard states the existing single-layout
and idle/restart limits; managed activation remains separate. Hermetic machine
fixtures establish selection behavior, not low-memory model fit. These findings
fit the existing craft guidance; no skill change is proposed.

Review follow-up found that equal model records did not prove installed-byte
reuse: template variants still fetched duplicate weights. Fetch regressions now
assert one model transfer, shared file identity, byte verification and rollback
after template failure. Model reuse stays within existing artifact receipts,
without a cache registry. The same review separated harness ownership from a
valid layout ID and extended viewport tests to wrapped selection rows. These
are corrections at the owning effect and rendering boundaries, not reasons to
add another configuration or lifecycle abstraction.

Trying Latest exposed a source-contract gap: the fake GitHub response represented
only numbered stable tags, while llama.cpp publishes its binary builds as
prereleases alongside semantic stable-release headings. The adapter now owns
that concrete release convention. Hermetic tests replay those release shapes
through the actual wizard resolver, including failed integrity and no-write
outcomes; a read-only upstream preview checked the fixtures against current
publication behavior. Existing testing guidance already calls for faithful
external contracts, so no skill change is warranted.

The presentation follow-up separated the persistent tabs and controls from
scrollable content, while keeping the same explicit-selection state machine.
One set of disclosure sections now feeds both plain CLI lines and styled review
blocks. Rendering checks cover actual terminal bounds and readable focus rather
than freezing an ANSI snapshot. Visual inspection removed a duplicated summary;
resize tests ensure hidden confirmation controls cannot be activated. Existing
unit-design and testing guidance covered the work; no skill change is warranted.

MacBook use exposed gaps that frame bounds alone did not establish: advancing
needed a visible control, scrolling needed ordinary keys, and duplicate
download totals hid whether weights were already present. The follow-up keeps
cache facts in the planner, projects one per-file status into CLI and TUI, and
retains the transfer summary when rows collapse. Regressions exercise visible
mouse positions and explicit-selection guards, skipped fixed templates, arrow
and wheel scrolling, and cache/no-cache disclosure. These are concrete usability
and integration checks under the existing guidance. Native inspection also
caught nested table wrapping; width regressions now preserve complete filenames,
statuses and table borders. No new craft rule is needed.

Shared-cache work initially grew a second implementation of HF's publication,
locking and partial-download lifecycle. The owner review corrected that boundary:
the official hf client now owns downloads and cache writes, while Temper owns
read-only preview, exact-byte verification and durable installation. Tests cover
that integration contract and the effects Temper still owns, including child
cancellation, corrupt bytes and cache/install removal independence. Downloader
support uses the existing hf or uv tool path without another dependency registry.
This applies the existing narrow-boundary and reliable-effects guidance; no new
skill mechanism is warranted.

Context review exposed a historical test condition being reused as a product
default. Maintained catalog layouts now own the native maximum; Selection owns
the accepted number and the existing lock/renderer carries it exactly. No new
capability registry or model-memory estimator was introduced. Frozen evidence
and old locks retain their original windows. Regressions check actual rendered
context arguments, independent mode choices, invalid input, exact resume and
unchanged weight identity. This applies the existing fact-ownership and
assertion-strength guidance; no skill change is needed.

## Catalog grouping and installed alternatives — 2026-09-26

The [setup contract](contracts/init.md) separates the models selected for
installation from the default local foreground. Data-modeling guidance kept
catalog labels, estimated memory tiers and editorial order outside execution
identity. The existing `local` selection/lock pair owns the default; alternatives
have their own exact pairs, without a second mutable default registry.

Reliable-effects guidance kept publication at the existing atomic configuration
boundary. Testing guidance directed regressions at independent template/context
choices, missing or ambiguous defaults, interrupted/refused saves, clean reruns
and offline preparation from every saved lock. A native terminal dry run checked
the grouped choices and per-model traversal. Existing guidance covered the
change; no skill change is warranted.


## Splash release integration — 2026-09-26

The [execution contract](contracts/execution-runtime.md) keeps draft identity,
source weights, derived tokenizer and native conversion cache at their own grains.
Splash owns architecture matching and conversion; Temper invokes the selected
release's metadata routine and owns exact inputs, publication and supervision.
This avoided copying upstream model-loading algorithms or introducing another
installer. The prebuilt archive exposed valid tar record padding that the shared
archive reader previously refused; bounded zero padding is now admitted with
checksum, payload and size-limit regressions.

Failure tests cover draft hash rejection, retained shared-cache ownership,
retry after cache repair, target reuse, concurrent preparation, changed metadata,
and exact frontend/native command ancestry. Harmless native child processes test
three-role ownership and shutdown. Existing craft guidance was sufficient.

The authorized native check then caught a source-format boundary omitted by
metadata-only tests: Splash requires a `model.json` descriptor to choose its
source loader. A focused assertion now checks that contract; startup diagnostics
are exposed through the router. After correction, the real 1.1.0 release served
short chat and tool requests and proved owned shutdown. The integration smoke
was kept separate from context-capacity and quality evidence.

## Writing companions and external GGUF drafts — 2026-09-27

The existing draft record now carries either an exact GGUF assistant or Splash's
safetensors pair. Data-modeling guidance kept its revision and file identities
independent of the target and serving software: changing a draft invalidates the
composition without changing those components. Rendering owns the native MTP or
DFlash flags, explicit draft GPU placement and F16 draft KV; callers do not
assemble alternate shell commands. Regressions check the selected file through
compilation and rendering, invalid material and independent invalidation.

The authorized Rapid comparison exposed a real child-process boundary missing
from software-only preparation: a separate crash-log helper. Reliable-effects
guidance kept recovery separate from a refused ownership observation. The
reviewed helper is admitted only by its script digest, selected interpreter,
engine parent and normal PID/start/argv/group identity. Tests cover changed
commands, wrong parents, reparenting after exit, duplicates and restarts. A native
request then completed with proved helper shutdown. Another transient llama.cpp
shutdown refusal remains unexplained; diagnostics now identify its process row,
without relaxing the guard or claiming the underlying cause was fixed.

Testing guidance kept forced-length acceleration, filled-input retrieval and
completed writing/tool work as separate evidence. Model mistakes survived some
speed/precision tuning, so the catalog descriptions retain concrete role limits.
The exact selected native configurations must survive editorial catalog naming
unchanged. Existing craft guidance was sufficient; no new skill rule or schema
registry was needed.


## Presets and user-owned layouts — 2026-09-28

Data-modeling guidance separated preset execution facts, catalog editorial copy,
user composition and observed activation. V3 authoring names the new concepts;
issued locks keep their historical meaning. Exact software closures are reusable
independently of a layout name. The [configuration contract](contracts/layouts.md)
is the single owner of these boundaries.

Reliable-effects guidance exposed an orphan risk in router-only recovery:
llama-swap starts engines in separate process groups. The managed path now
records each engine lifetime before exec and verifies kernel identity and exact
argv before recovery signals. The launcher replaces itself; it is no additional
daemon. Interrupted desired-state commits and provider responses are tested at
their durable boundaries, separately from fixed-process Field Kit supervision.

Testing guidance kept catalog selection, mixed rendering, memory predictions,
disposable-process ownership tests and native serving as different claims.
The [authorized bounded native smoke](design/managed-layout-smoke.md#observed-result)
exposed a mismatched fake: v260 omits an empty in-flight request list. The
regression now uses the producer's actual wire form. Idle unload also exposed
an `ESRCH` race between two reads; bounded complete-snapshot retries preserve
ownership refusals while handling that normal lifecycle transition. Cold-cache
timeout and startup admission remain limits of the observation. Existing craft
guidance was sufficient; no skill edits or new evidence registry were needed.

## Alpha schema cleanup and release — 2026-09-29

Data-modeling guidance kept historical context identities and issued snapshots
unchanged while removing obsolete runtime readers. Testing used the public
compiler and configuration commands across Field Kit's frozen matrix. A real-host
preview exposed an output-parent precondition missing from the fake; the preview
now uses the existing private directory without writing a lock. Reliable-effects
guidance kept the bootstrap pin tied to the downloaded signed archive and checked
fresh setup plus unchanged replay. [Delivery verification](PLAN.md#alpha11-delivery)
records the result. Existing guidance was sufficient; no skill change is proposed.
