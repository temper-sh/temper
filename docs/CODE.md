# Maintainer code map

This document answers the maintenance question: **where does a change go, and
what else must move with it?** It maps the current Go implementation without
replacing its product contracts or narrating every function.

Use the authority stack in this order:

1. `docs/SPEC.md` defines product behavior and non-negotiable boundaries.
2. `docs/PLAN.md` defines sequencing, gates, and accepted engineering work.
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
| `cmd/temper` | User-facing CLI for manifest/lock, artifacts, rendering, checks, software lifecycle, machine facts, Field Kit host primitives, probes, and catalog update | Constructs upstream readers, machine detectors, the compiled software adapter family, Field Kit binding/probe commands, catalog trust, and catalog transport |
| `cmd/temper-catalog` | Release-only catalog signing and verification tool | Constructs signing/verification capabilities; private seed bytes enter only through stdin |
| `cmd/temper-release` | Maintainer-only deterministic binary/archive builder | Cross-builds the version-injected macOS ARM64 binary, discovers the linked module graph, collects root license/notice bytes, and conditionally commits the release ZIP and checksum; it never signs, notarizes, or publishes |

`cmd/` is allowed to know concrete implementations. Domain and use-case
packages are not. Adding a provider or transport therefore changes wiring at
the composition root; it must not add provider switches to CLI verbs.

`internal/softwarecmd` is the public command edge for the nested `temper
software ...` surface. It owns flags, exit codes, stable `RESULT`/detail lines,
and translation into the software use-case packages. It does not own install,
check, removal, or catalog policy.

## Public operation paths

| Operation | Orchestrator | Decisions and schemas | Reads/effects | Contract |
|---|---|---|---|---|
| `temper resolve` | `internal/resolve` | `manifest`, `lockfile`, `pinning` | `upstream`/`huggingface`; `lockstore` atomic commit | `docs/contracts/resolve.md` |
| `temper update` | `internal/update` | `manifest`, `lockfile`, `pinning`, update gates | `upstream`/`huggingface`; `lockstore` atomic commit | `docs/contracts/update.md` |
| `temper fetch` | `internal/fetch` | `manifest`, `lockfile`, `artifactset`, `patch` | upstream byte reads; immutable artifact-set publication | `docs/contracts/fetch.md` |
| `temper apply` | `internal/apply` | `manifest`, `lockfile`, `artifactset`, `render` | artifact verification; staged generation and atomic `rendered/current` switch | `docs/contracts/apply.md` |
| `temper check` | `internal/check` | `manifest`, `lockfile`, `artifactset`, `budget` | machine and artifact reads only | `docs/contracts/check.md` |
| `temper software catalog update` | `software/catalogupdate` through `softwarecmd` | `software/catalog`, `catalogpublication`, adapter capability registry | signed HTTPS source; immutable catalog store and active-pointer commit | `docs/contracts/software-catalog-update.md` |
| `temper software install` | `software/install` through `softwarecmd` | `software/lockfile`, `installplan`, `receipt`, `rootstate` | receipt/state stores; compiled installation adapter effects | `docs/contracts/software-install.md` |
| `temper software check` | `software/check` through `softwarecmd` | `software/lockfile`, `checkplan`, `receipt`, `rootstate` | stores and provider inspection; no writes | `docs/contracts/software-install.md` |
| `temper software remove` | `software/remove` through `softwarecmd` | `software/lockfile`, `removeplan`, `receipt`, `rootstate` | prepared authority, compiled adapter removal, receipt/state commits | `docs/contracts/software-install.md` |
| `temper probe serve` | `internal/probecmd` | exact software receipt/lock and rendered-generation admission | foreground loopback process group; dry-run is read-only | `docs/contracts/probe-serve.md` |
| `temper field-kit bind` | `internal/fieldkitcmd` + `internal/fieldkitbinding` | manifest/software locks, receipts, canonical machine facts | explicitly named Temper state; pure binding after reads | `docs/contracts/field-kit.md` |
| Qualification documents | `internal/qualification` | exact references/index, common profile/evidence envelope, canonical witness-scope keys, independent immutable qualification/lifecycle transitions, machine buckets, model artifacts, engines, model runtimes/performance, tools, modes, activities, and exact bundle loading | read-only reuse of software-supply target/catalog constants; callers supply index/document bytes and canonical facts, and all parsing, hashing, validation, loading, transition/composition checks, and matching are pure | `docs/design/qualification-catalog-schema.md` |

An internal package not listed as a public operation is usually a decision or
boundary collaborator. It does not become a user-facing surface merely because
it is executable in a test.

## Package neighborhoods

### Manifest, model artifacts, and rendering

| Package | Owns |
|---|---|
| `internal/manifest` | Strict user-owned `temper-manifest/v1` parsing and invariants |
| `internal/lockfile` | Strict resolved model lock and its canonical identities |
| `internal/lockstore` | Concurrency-safe model-lock snapshots and atomic replacement |
| `internal/pinning` | Resolution and validation of exact upstream layout pins |
| `internal/upstream` | Narrow model-metadata/byte-read contracts consumed by resolution and fetch |
| `internal/huggingface` | Hugging Face transport adaptation; external response types stop here |
| `internal/patch` | Pinned patch-source parsing and deterministic patch application |
| `internal/artifactset` | Immutable content-addressed layout-set identity and verification |
| `internal/render` | Pure construction of the complete llama-swap/Pi configuration bundle |
| `internal/render/engine` | Closed pure engine-launch family, typed adapters, specialized command builders, and safe llama-swap shell serialization |
| `internal/runtimeconfig` | Canonical generation-owned, receipt-resolved executable requirements shared by render and probe |
| `internal/budget` | Pure resident-wall arithmetic |
| `internal/machine` | Read-only host target, hardware, and memory facts |
| `internal/datadir` | Validation of the explicit isolated Temper root boundary |

### Field Kit host primitives

| Package | Kind | Owns |
|---|---|---|
| `internal/fieldkitcmd` | stable command edge | reads explicitly named Temper material and emits the canonical binding |
| `internal/fieldkitbinding` | pure identity | exact executing material across machine, binary, locks, receipts, and rendered generation |
| `internal/probecmd` | controlled effect boundary | admission and process-group lifecycle for one exact receipt/generation-bound loopback router |
| `internal/releaseartifact` | pure release document | strict release SemVer, deterministic ZIP names/order/modes/timestamps/checksum, and stable third-party notice rendering |

Current Field Kit source and runtime live in the adjacent repository and call
only public Temper primitives. Field Kit catalogs, question packages, sessions,
protocols, evidence, and reports are not copied into this repository.

Release assets are assembled only by `cmd/temper-release` under
`docs/contracts/release.md`. The command refuses non-ARM64 Mach-O input,
module replacements, missing root license/notice material, and same-version
artifact collisions. Developer ID credentials and publication authority exist
only in the tag workflow.

The use-case packages `resolve`, `update`, `fetch`, `apply`, and `check`
compose these capabilities. Cross-use-case facts belong in the packages above;
workflow-only decisions stay with the use case.

### Software supply and exact desired state

| Package | Owns |
|---|---|
| `internal/software` | Provider-neutral shared values such as targets, candidates, and artifacts |
| `software/archive` | Shared bounded tar.gz inspection, safe extraction, and canonical installed-tree inventory for isolated adapters |
| `software/catalog` | Strict software-supply catalog parsing and validation |
| `software/version` | Closed SemVer/PEP 440/opaque/git version semantics |
| `software/policy` | Pure catalog recipe and constraint policy |
| `software/selection` | Deterministic provider-candidate selection |
| `software/testedstatus` | Derived tested/untested/known-bad/outside-policy status |
| `software/resolve` | Catalog + provider reads + selection + one software-lock commit |
| `software/lockfile` | Exact desired software closure and provenance |
| `software/lockstore` | Concurrency-safe software-lock snapshots and atomic replacement |

Catalog policy never proves what is installed. The lock is desired state, a
receipt is observed history, and root state is operation/share authority.

### Software adapter family

`internal/software/adapter` owns the common resolver/installation contracts,
descriptor validation, and keyed family dispatch. Concrete provider knowledge
stays in member packages:

- `adapter/homebrew` translates Homebrew metadata and controlled process
  behavior.
- `adapter/uv` translates version-matched uv/PEP 751 data into an exact managed
  Python closure and installs that locked closure into an inspected immutable
  environment using its exact managed runtime and local hashed wheelhouse.
- `adapter/upstreamrelease` resolves, installs, inspects, and removes isolated
  verified release archives.

Both isolated effect members delegate tar.gz path, bound, mode, link, hash,
extraction, and tree-inventory semantics to `software/archive`. They do not
share an installed interpreter or publication lifecycle.

The production `temper` installation-effect family wires the isolated
`upstreamrelease` and `uv` members. Existing resolver code or tests do not
authorize a CLI fallback to Homebrew, an unknown adapter, ambient Python, or
an ambient package index.

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

### Catalog publication

| Package | Owns |
|---|---|
| `software/catalogpublication` | Pure detached-signature envelope parsing and verification |
| `software/catalogtrust` | Public verification keys compiled into the binary |
| `software/catalogsource` | Bounded read-only catalog transport |
| `software/catalogreader` | Verified active-or-bootstrap catalog selection |
| `software/catalogbootstrap` | Embedded signed fallback publication |
| `software/catalogstore` | Immutable snapshots and conditional active digest pointer |
| `software/catalogupdate` | Verify → capability/rollback checks → one active-pointer commit |
| `software/catalogsigning` | Release-side signing, verification, and conditional output commit |

Publication, activation, resolution, and installation are separate operations.
No package in this neighborhood is permission to publish the staged live
catalog.

## Durable state layout

Paths below are relative to the explicit isolated Temper root unless marked
caller-owned.

| State | Path/identity | Writer and rule |
|---|---|---|
| User choices | caller-owned `manifest.yaml` | Wizard writes only when absent; mechanical code never rewrites it |
| Resolved model pins | caller-owned `manifest.lock.yaml` | `resolve`/`update` through `internal/lockstore`; one atomic replacement |
| Model artifact set | `artifacts/layouts/<layout>/<digest>/` | `fetch`; immutable content-addressed publication |
| Render generation | `rendered/generations/<digest>/` | `apply`; immutable generation |
| Current render | `rendered/current` | `apply`; atomic relative symlink switch after validation |
| Software lock | caller-owned `software.lock.yaml` | `software/resolve` through `software/lockstore`; installation only consumes it |
| Catalog snapshots | `software/catalog/snapshots/<digest>/` | `catalogstore`; immutable verified publication |
| Active catalog | `software/catalog/active` | `catalogupdate`; exact digest plus newline in a regular file |
| Installation receipt | `software/installations/<id>/installation-receipt.yaml` | `receiptstore`; canonical conditional commit/removal |
| Root software authority | `software/state.yaml` | `statestore`; each prepared/finalized operation or claim transition commits here atomically |
| Current Field Kit state | owned by the adjacent Field Kit runtime outside this repository | Field Kit invokes Temper stores only through public root-explicit verbs |

No state path in this table points at the live legacy installation before the
release cutover gate.

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

### Extend qualification profiles

1. Refine `docs/design/qualification-catalog-schema.md`; do not introduce a
   generic untyped `spec` escape hatch.
2. Add the typed document to `internal/qualification` with canonical fake
   bytes and strict refusals.
3. Keep selection, consent, install authorization, Labs state, and Field Kit
   session data out of the package.
4. Extend the index loader only after each referenced typed document surface
   exists. It verifies machine buckets and all six profile kinds, including
   exact runtime/mode composition and activity narrowing. Public evidence is
   accepted only with a recomputed typed scope; qualified profiles require the
   complete schema-specific review gates, runtime task-quality evidence, and
   an available exact dependency closure. Recommendations still fail closed.
5. Do not build the product-promotion compiler until Labs adopts its writer contract under
   explicit cross-repository authorization.

## Keeping this map useful

Update documentation in the same commit as the change it explains:

| Change | Documentation home |
|---|---|
| Product behavior or boundary | `docs/SPEC.md` |
| Milestone, gate, or sequencing | `docs/PLAN.md` |
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
