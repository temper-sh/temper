# Contributing and building from source

For normal installation, use [Homebrew](../README.md#get-started). To work on
Temper itself, clone this repository and install Go 1.26 or newer. On an Apple
Silicon Mac, build from the repository root:

```sh
go build -o build/temper ./cmd/temper
./build/temper --help
```

Run `go test ./...` for code changes. Tests use isolated fixtures and do not
start your local model service. Documentation changes need link and command
checks; they do not need a model run. See the
[release reference](contracts/release.md) for packaging and signing.

Use [issues](https://github.com/temper-sh/temper/issues) for bugs and proposals.
Include the Temper version, machine, command and error when reporting a problem.
For a code change, the map below locates the relevant implementation and tests.

## Preview a catalog source

Catalog sources live in the Results site repository
(`catalog/sources/apple-silicon.json`). To try an edited source, open it in a
separate configuration root:

```sh
go run ./cmd/temper init \
  --root "$HOME/.temper-dev" \
  --catalog ../temper-sh.github.io/catalog/sources/apple-silicon.json
```

`--catalog FILE` uses the local file as is, with no signature check. Without
it, setup uses the root's verified catalog or downloads the published one.
`--root` keeps this preview apart from your everyday configuration; add
`--dry-run` to review without saving or preparing. Tests never read a live
source: they use the frozen copies in `internal/testfixture/testdata/`.

A root written by a Temper before 0.1.0 has no `temper-root.json`; 0.1.0
refuses it. Use a fresh root.

## Compatibility

Temper 0.1.x promises two things: the published catalog files (catalog,
channel, signatures) and the durable part of a root (`configuration.json` with
its saved locks, the `catalog/` store, `temper-root.json`). Everything else in
a root is cache that a later Temper may reuse, rebuild or delete. Command
output, `machine facts`, `temper software` with its lock and result lines, and
`catalog compile|describe` may change in any release. Callers such as Field
Kit, Labs and Workshop pin an exact Temper version.

## Maintainer code map

This document answers the maintenance question: **where does a change go, and
what else must move with it?** It maps the current Go implementation without
replacing its product contracts or narrating every function.

Use the authority stack in this order:

1. `docs/SPEC.md` defines product behavior and non-negotiable boundaries.
2. The local `docs/PLAN.md`, when present, defines sequencing, gates, and accepted
   engineering work. Planning documents are not tracked or published.
3. `docs/design/` defines persistent schemas and cross-component contracts.
4. `docs/contracts/` defines public command behavior and stable output.
5. This file locates the implementation of those decisions.
6. Package comments and black-box tests document the narrow Go surface.

If code and a higher contract disagree, do not “document the code” to make the
disagreement disappear. Fix the implementation or explicitly revise the
contract through its review gate.

## How to follow a change

Start at the public edge, then follow imports inward:

```text
cmd/ composition root
        |
        v
command parsing and stable output
        |
        v
use-case orchestrator  ---> reads and effect adapters
        |
        v
pure documents, validation, plans, and rendering
        |
        v
declared durable transition(s), with irreversible effects behind authority
```

The normal investigation order is:

1. Read the relevant contract or schema document.
2. Find the command edge in `cmd/temper` or `internal/softwarecmd`.
3. Open the use-case package named by that edge.
4. Identify its pure decision package and its read/effect collaborators.
5. Read the black-box test for the behavior and the store/adapter test for the
   real boundary.

This order avoids treating flag parsing, filesystem mechanics, or provider
syntax as the owner of a product rule.

## Executables and composition roots

| Executable | Role | Wiring boundary |
|---|---|---|
| `cmd/temper` | User-facing CLI for guided setup, layouts, execution locks, software lifecycle, machine facts, and catalog update | Constructs setup UI/reads, the typed preparation and execution effects (software command, HF-backed model fetch, supervised serving, binary identity), machine detectors, the compiled software adapter family, catalog trust, and catalog transport |
| `cmd/temper-catalog` | Release-only catalog publisher and verifier (`publish`, `verify-publication`, `availability`) | Constructs the signing tool and production trust; private seed bytes enter only through stdin |
| `cmd/temper-release` | Maintainer-only deterministic binary/archive builder | Cross-builds the version-injected macOS ARM64 binary, discovers the linked module graph, collects root license/notice bytes, and conditionally commits the release ZIP and checksum; it never signs, notarizes, or publishes |

`cmd/` is allowed to know concrete implementations. Domain and use-case
packages are not. Adding a provider or transport therefore changes wiring at
the composition root; it must not add provider switches to CLI verbs.

`internal/softwarecmd` is the public command edge for the nested `temper
software ...` surface. It owns flags, exit codes, stable `RESULT`/detail lines,
and translation into the software use-case packages. The same install, check
and remove operations serve in-process callers (preset preparation and the
execution commands) that already hold an exact lock; nothing re-enters the CLI.
It does not own install, check, removal, or catalog policy.

## Public operation paths

| Operation | Orchestrator | Decisions and schemas | Reads/effects | Contract |
|---|---|---|---|---|
| `temper init`, `configure` | `internal/setupcmd` | `setup`: selected exact presets with the user's choices, named layouts, the software setting, memory/download plan | machine/disk/catalog reads, `setupui` editors, revision-checked atomic configuration save, explicit shared `preset.Prepare` | `docs/contracts/init.md` |
| `temper update` | `setupcmd.Update` | `setup.BuildPreset` (origin preset + choices), `setup.WithNewestSoftware`, `setup.Changes` | newest catalog download and store, the same save and preparation as `configure` | `docs/contracts/init.md` |
| `temper layout activate/status/stop` | `setupcmd.Layout`, `managed.Reconcile` | frozen desired/current jobs, pure composed rendering | exact preparation checks, activation journal, launchd, owned process observation and recovery | `docs/contracts/layouts.md` |
| `temper catalog compile` | `internal/catalogcmd` | `catalog`: four native records, selected preset closure and execution identity | explicit local file reads; atomic lock publication without replacement | `docs/contracts/execution-lock.md` |
| `temper execution configure` | `internal/catalogcmd` | `catalog.ConfigureExecution`: explicit context/output/Splash memory limits and compute selection | immutable derived lock publication | `docs/contracts/execution-runtime.md` |
| `temper catalog describe` | `internal/catalogcmd` | `catalog.DescribePreset`: authored preset copy and optional assessment link | explicit authoring-file read, directory writer lock, validated atomic replacement; dry run writes nothing | `docs/contracts/execution-lock.md` |
| `temper execution prepare/render/serve/remove/paths` | `catalogcmd.Runtime` | in-memory projections of one exact execution lock | software install/check/remove, `fetch`, Splash assembly, `apply` generation commit, content hashing, Field Kit binding; one supervised foreground router | `docs/contracts/execution-runtime.md` |
| `temper software install` | `software/install` through `softwarecmd` | `software/lockfile`, `installplan`, `receipt`, `rootstate` | receipt/state stores; compiled installation adapter effects | `docs/contracts/software-install.md` |
| `temper software check` | `software/check` through `softwarecmd` | `software/lockfile`, `checkplan`, `receipt`, `rootstate` | stores and provider inspection; no writes | `docs/contracts/software-install.md` |
| `temper software remove` | `software/remove` through `softwarecmd` | `software/lockfile`, `removeplan`, `receipt`, `rootstate` | prepared authority, compiled adapter removal, receipt/state commits | `docs/contracts/software-install.md` |

An internal package not listed as a public operation is usually a decision or
boundary collaborator. It does not become a user-facing surface merely because
it is executable in a test.

## Package neighborhoods

### Presets, configuration and managed availability

`catalog/compile.go` compiles v1 presets directly into self-contained execution
locks. Locks store no fingerprint; `catalog.SameExecution` compares what runs
inside one Temper, and recommendation data and manual copy never change it.
Each schema has one accepted form; there are no migration or compatibility exports.
`catalog/presentation.go` owns license explanation links by declared identifier,
with the pinned model card as the fallback for custom licenses. Setup reads
the selected artifact's license; terminal views add hyperlinks and text reviews
include the URL.

`setupui/presets.go` owns one revocable draft across General/Specialist/Other, Layouts
and review. `presets_navigation.go` owns input and cancellable previews;
`presets_view.go` shares tabs, cards and download tables with the existing UI.
It receives resolved choices and a preview callback, with no effect or network
ownership. `setupcmd/configure.go` reads and resolves inputs, reviews
and saves. `setup/configuration.go` owns references, strict parsing, revision
checks and atomic configuration publication. `composition.go` budgets startup
sets separately from alternatives. Existing setup files retain historical pair
operations and explicit import support; they are not the current editor.

`preset` owns per-preset exact software closure splitting, installed inspection,
preparation and verification through existing software/artifact primitives.
Equivalent closures use one digest-based installation across layouts. It does
not start engines or choose layout membership.

`managed.Render` composes exact commands with independent engine versions,
startup/default and idle groups. `managed.Reconcile` commits desired state before
job effects and observes both sides of interrupted transitions. `managed.Launchd`
is the macOS effect adapter; it has no automatic restart or login-agent side
effect. `probecmd/managed.go` reuses kernel executable/argv, ancestry and listener
observations while allowing fresh lifetimes after idle eviction. It does not
relax the existing fixed-process Field Kit supervisor.

The generated internal launcher records its PID/group/start/argv and execs the
engine. Its durable identity allows orphan recovery after router exit without
another background daemon. Launch specs and rendered configs are immutable;
activation and per-command process records are separate mutable runtime state.
Tests live beside these boundaries and inject interruptions, stale writers,
foreign processes and unknown activity. Native launchd/model acceptance is a
separate [bounded smoke](design/managed-layout-smoke.md).

### Manifest, model artifacts, and rendering

| Package | Owns |
|---|---|
| `internal/manifest` | In-memory layout model projected from an execution lock, with strict invariants, including external foreground for utility profiles |
| `internal/lockfile` | Strict resolved model lock and its canonical identities |
| `internal/upstream` | Narrow byte-read contract consumed by fetch |
| `internal/huggingface` | Hugging Face file transport; external response types stop here |
| `internal/hfcache` | Shared model-cache inspection and the user's hf invocation; HF owns cache writes, locking and recovery |
| `internal/fetch` | One layout's exact artifact set: cache lookup, template bytes, staged verification and atomic publication |
| `internal/apply` | One verified render generation and the atomic `rendered/current` switch |
| `internal/patch` | Pinned patch-source parsing and deterministic patch application |
| `internal/artifactset` | Immutable target/draft/projector material identity, verification and receipt-based discovery of reusable model hashes |
| `internal/splash` | Splash assembly identity, local metadata derivation using the selected release, atomic preparation and verification; source models stay in `artifactset` |
| `internal/render` | Pure construction of the complete llama-swap configuration bundle and runtime requirements |
| `internal/render/engine` | Closed pure engine-launch family, including llama chat/vision/reranking and CPU CrispEmbed reranking; typed adapters and safe llama-swap shell serialization |
| `internal/runtimeconfig` | Canonical generation-owned, receipt-resolved executable requirements shared by render and serving |
| `internal/budget` | Pure resident-wall arithmetic |
| `internal/machine` | Read-only host target, hardware, and memory facts |
| `internal/datadir` | Validation of the explicit isolated Temper root boundary |

### Field Kit host primitives

| Package | Kind | Owns |
|---|---|---|
| `internal/fieldkitbinding` | pure identity | exact executing material across machine, binary, locks, receipts, and rendered generation |
| `internal/probecmd` | controlled effect boundary | receipt/generation admission, kernel executable identity and supervised router/engine groups |
| `internal/releaseartifact` | pure release document | strict release SemVer, deterministic ZIP names/order/modes/timestamps/checksum, and stable third-party notice rendering |

Current Field Kit source and runtime live in the adjacent repository and call
only public Temper primitives. Field Kit catalogs, question packages, sessions,
protocols, evidence, and reports are not copied into this repository.

Direct-download release assets are assembled only by `cmd/temper-release` under
`docs/contracts/release.md`. The command refuses non-ARM64 Mach-O input,
module replacements, missing root license/notice material, and same-version
artifact collisions. Developer ID credentials exist only in the tag workflow.
`Formula/temper.rb` invokes that builder and notice packaging for source installs;
`.github/workflows/homebrew.yml` uses Homebrew itself to build, test and package
bottles. Bottle assets precede the formula's default-branch update. The
[release contract](contracts/release.md#homebrew-formula-and-bottles) owns this
distribution path and its publication/retry boundaries.

Preset preparation and the execution commands compose `fetch` and `apply`
with the software operations. Cross-use-case facts belong in the packages above;
workflow-only decisions stay with the use case.

### Software supply and exact desired state

| Package | Owns |
|---|---|
| `internal/software` | Provider-neutral shared values such as targets and artifacts |
| `software/archive` | Shared bounded tar.gz inspection, safe extraction, and canonical installed-tree inventory for isolated adapters |
| `internal/catalog` | Maintained source records, latest/tested resolution, exact execution-lock compilation and derived installer inputs |
| `internal/catalog/distribution` | Current signed publication verification, bounded updates, verified local reads and deliberate rollback; one atomic active/highest-accepted state commit |
| `software/version` | Closed SemVer/PEP 440/opaque/git version semantics |
| `software/lockfile` | Exact desired software closure and provenance |
| `software/lockstore` | Read-only software-lock file snapshots for the `temper software` command edge |

Catalog policy never proves what is installed. The lock is desired state, a
receipt is observed history, and root state is operation/share authority.

`internal/catalogcmd` exposes catalog update, inspection, explicit presets,
compilation from a verified store and rollback. Distribution reuses the existing
catalog trust root, signature envelope and HTTPS transport. Current state lives under `ROOT/catalog`. The separate software-catalog parser,
store, updater and generic candidate policy have been removed.

### Software adapter family

`internal/software/adapter` owns the exact installation contracts,
descriptor validation, and keyed family dispatch. Concrete provider knowledge
stays in member packages:

- `adapter/python` installs an exact locked Python closure into an inspected immutable
  environment using its exact managed runtime and local hashed wheelhouse.
- `adapter/upstreamrelease` discovers latest or exact numbered/semantic GitHub releases,
  verifies archive identities/inventory, and installs, inspects and removes
  private release archives.

Both isolated effect members delegate tar.gz path, bound, mode, link, hash,
extraction, and tree-inventory semantics to `software/archive`. They do not
share an installed interpreter or publication lifecycle.

The production `temper` installation-effect family wires the isolated
`upstreamrelease` and `python` members. There is no automatic fallback to Homebrew, an unknown adapter, ambient Python, or
an ambient package index.

Model-download support is a separate, explicit boundary: `hfcache` runs the
user's own official `hf` executable; Temper never installs or provisions it.
The setup preview refuses preparation with a one-line install hint while model
files remain to download and `hf` is absent. This does not install serving
runtimes or change their locked closures. Fetch independently verifies the
resulting model bytes before publication.

To add a member, implement the existing narrow adapter role in its own
package, add its descriptor to compiled capability validation, and wire the
concrete member in `cmd/`. Change `adapter` itself only when the common
contract changes for every member.

### Software planning, evidence, and mutation

| Package | Kind | Owns |
|---|---|---|
| `software/installplan` | pure | Exact provider-neutral install groups, ownership, claims, and receipt intent |
| `software/removeplan` | pure | Provenance-guided removal and last-claim decisions |
| `software/checkplan` | pure | Read-only drift/finding classification |
| `software/receipt` | pure document | Canonical per-installation observed history |
| `software/rootstate` | pure document/state machine | Prepared operation authority, fences, and root-wide shared claims |
| `software/receiptstore` | effect boundary | Derived receipt paths and conditional atomic commits/removal |
| `software/statestore` | effect boundary | The one atomic root-state commit point |
| `software/install` | orchestrator | Read → plan → prepare → provider effect → inspect → receipt → finalize |
| `software/check` | read orchestrator | Read/inspect → pure analysis; never commits |
| `software/remove` | orchestrator | Read → plan → prepared authority → provider removal → verify → release/finalize |

The order in the last three rows is part of the reliability contract. Do not
move provider effects ahead of prepared authority or turn check findings into
writes.

### Current catalog publication

One catalog distribution owner uses these signature and transport primitives.

| Package | Owns |
|---|---|
| `software/catalogpublication` | Pure detached-signature envelope parsing and verification |
| `software/catalogtrust` | Public verification keys compiled into the binary |
| `software/catalogsource` | Bounded read-only catalog transport |
| `software/catalogsigning` | Release-side signing used by `temper-catalog publish` |
| `catalog/distribution` | Channel and snapshot verification, the local store and its floor, publication-tree checks |

Publication, activation, resolution, and installation are separate operations.
No package in this neighborhood is permission to publish the staged live
catalog.

## Durable state layout

Paths below are relative to the explicit isolated Temper root unless marked
caller-owned.

| State | Path/identity | Writer and rule |
|---|---|---|
| Root format marker (durable) | `temper-root.json` | `datadir.Ensure`/`Mark` on every writer; `datadir.Check` on every open |
| Selected presets and layouts (durable) | `configuration.json` | `configure`, `update`; atomic revision-checked replacement under the root lock |
| Managed activation | `managed/activation.json` | `managed.Reconcile`; desired/current frozen jobs, separate from configuration editing |
| Managed render and job | `managed/generations/<digest>/` | Immutable config/plist; suspend/shutdown recovery records remain until settled |
| Engine launch ownership | `managed/commands/<digest>/launch.json` and `process.json` | Immutable command; launcher atomically records each lifetime before exec |
| Model artifact set | `artifacts/layouts/<layout>/<digest>/` | `fetch`; exact cache references and local templates; verification receipt refreshed after cache restoration |
| Shared HF model cache | Outside the Temper root; HF environment overrides or `~/.cache/huggingface/hub` | Official HF client; Temper verifies and references cached files, never copies, hard-links or prunes them |
| Render generation | `rendered/generations/<digest>/` | `apply` through `execution prepare/render`; immutable generation |
| Current render | `rendered/current` | `apply`; atomic relative symlink switch after validation |
| Software lock | caller-owned `software.lock.yaml` for `temper software`; preparation and execution derive it in memory from the execution lock | Installation only consumes it |
| Current maintained catalog (durable) | `catalog/snapshots/<digest>/`, `catalog/state.json`, `catalog/channel/` | `catalog/distribution`; immutable snapshots, one atomic active/latest state commit, then the newest verified channel (the floor) |
| Router log | `managed/logs/router.log` | launchd output; emptied before each start |
| Installation receipt | `software/installations/<id>/installation-receipt.yaml` | `receiptstore`; canonical conditional commit/removal |
| Root software authority | `software/state.yaml` | `statestore`; each prepared/finalized operation or claim transition commits here atomically |
| Current Field Kit state | owned by the adjacent Field Kit runtime outside this repository | Field Kit invokes Temper stores only through public root-explicit verbs |

Rows marked durable are promises (see Compatibility); every other path is
cache.

## Common edit recipes

### Change a canonical document

1. Revise or confirm the owning design contract first.
2. Change the typed document, strict parser, validator, and canonical encoder
   in the owning package.
3. Add a small canonical fake fixture and refusal cases for ambiguous bytes.
4. Update every exact-reference or digest consumer.
5. Use a new schema/revision when the old bytes or meaning must remain valid;
   do not add silent compatibility aliases.

### Add or change a CLI operation

1. Freeze flags, mutation boundary, exit behavior, and stable output in
   `docs/contracts/`.
2. Put product behavior in one `internal/` use-case package.
3. Keep parsing, wiring, and output formatting at the command edge.
4. Test the use case through its public Go surface and add one command-level
   wiring/output test.
5. Update this operation table and README only when the operator entry path
   changes.

### Change a mutating workflow

1. Keep the decision in a pure plan where possible.
2. Enumerate durable commits and external-effect boundaries before editing.
3. Stage and validate before each declared durable transition; record
   authority before an irreversible provider effect.
4. Add failure injection after each meaningful durable/effect boundary,
   restart through the ordinary entry point, and prove convergence.
5. Re-prove `--dry-run` purity and a clean second run.

### Change installable catalog records

Use the execution-lock contract and the current software source policy. Keep
source facts separate from resolved installation details. Results owns public
assessment; reference applicable Labs or Field Kit evidence through ordinary
review. No qualification package or product-promotion compiler is involved.

## Keeping this map useful

Update documentation in the same commit as the change it explains:

| Change | Documentation home |
|---|---|
| Product behavior or boundary | `docs/SPEC.md` |
| Milestone, gate, or sequencing | Local `docs/PLAN.md` (not tracked) |
| Persistent schema or cross-component contract | `docs/design/` |
| Public flags, output, refusal, or mutation behavior | `docs/contracts/` |
| Package boundary, operation path, dependency direction, or durable path | this file |
| Operator entry point or current shipped capability | `README.md` |
| Local algorithm or narrow exported surface | Go package comment, symbol comment, and black-box test |

A code change must update this map when it adds, removes, renames, or moves a
package; changes a public operation path; changes which package owns a
decision or effect; or changes durable state layout. Purely internal refactors
behind the same package surface should leave this file alone.

The maintenance check is simple: from this file, can a new contributor locate
the public edge, decision owner, effect boundary, durable state, and primary
test for the changed behavior? If not, the change is not documented yet.
