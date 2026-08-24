# Software supply + lock — C4/C5 design

Status: **approved by owner**, extended 2026-08-21 for experiment locks and
layered installations. The executable M2 Phase A shared
resolver now consumes this surface: strict catalog/lock parsing and validation,
normalized target selection, compiled adapter descriptor matching,
provider-neutral candidate closures, SemVer/PEP 440 policy selection, closure
invariants, canonical digests, and the dry-run/concurrency-safe atomic lock
transaction. The Homebrew candidate protocol and strict JSON translator are
now executable behind an injected runner, including a controlled production
non-shell process edge. Four-way tested-status derivation is executable and is
returned by software resolution without being persisted. The deterministic
`upstream-release` resolver and its production HTTPS reader plus isolated
installer/inspector/remover are executable; their archive and effect contracts
are covered hermetically and the real isolated lifecycle passes through the
public software commands. The uv reader and remaining concrete installation
members remain pending. The internal signed
channel/catalog verification, immutable store, rollback and equivocation
policy, capability gate, dry-run, and active-pointer transaction are now
executable and hermetically tested; a read-only consumer verifies the active
snapshot or an injected embedded bootstrap before use. A bounded read-only
HTTPS catalog source now implements the publication layout below. Actual
production trust/bootstrap bytes, the channel root, and public catalog-command
wiring remain release work. Later schema changes still require review.

The isolation-first curation policy applies to every Temper installation, not
only the Field Kit base. On macOS, the deliberately small shared bootstrap
layer may use Homebrew for `uv` and `hf`; uv then owns exact Python runtimes and
application environments below that layer. The Field Kit serving base needs
`llama-swap` and `llama-cpp`; the 2026-08-24 method review selects isolated
`release-artifact` installation for both on macOS Apple Silicon. The concrete
source/adapter is implemented and its explicitly authorized scratch round-trip
passes. Exact recipes may now use those reviewed artifact identities, while an
`exact-tested` claim still requires stable Results or Field Kit evidence.
`rapid-mlx` and `mlx-dspark` remain supported, non-default Python packages.
Their explicit `python-environment`/`uv` recipes exercise PEP
440 and exact Python/MLX closure control. Nothing selects or installs either
package unless the user or a reviewed packet explicitly requests it.

The Phase A resolver answers one narrow question:

> Given a logical package, an explicit installation method, a signed catalog snapshot,
> and target-machine facts, which exact software closure should Temper install?

It does not install that closure. Resolution is a read plus pure selection and
one lock-file commit. Installation and its receipt are the following M2 Phase B
slice. Its schema-independent pure planner now handles named installations,
verified base requirements, direct or catalog-backed experiment provenance,
prepared recovery, and root-wide shared claims. The approved C6 receipt,
root-state, CLI output, and packet-identity surface are in
`../contracts/software-install.md`. Strict canonical receipt/root-state
documents, derived-path conditional atomic stores, and internal keyed-adapter
install orchestration are now executable. The read-only check analyzer and
lock/store/adapter reader are executable as well. Provenance-guided removal now
has a pure planner, a serialized active-to-retiring final-release transition,
conditional receipt deletion, keyed adapter orchestration, and explicit-rerun
recovery. The public commands and packet binding are executable on the Temper
side. The first concrete isolated member is the `upstream-release` adapter;
its public composition and real scratch gate pass. Field Kit-side stage
integration and the remaining concrete adapters remain.

## Facts and their owners

| Fact | One owner | Why it is not stored elsewhere |
|---|---|---|
| logical package identity | supply catalog package record | independent of package-manager spelling |
| portable installation strategy | supply catalog method record | reviewed intent, not detected machine state |
| canonical method + target → adapter choice | supply catalog target binding | one catalog policy; no environment-driven guessing |
| adapter protocol, effect model, capabilities | supply catalog adapter record, checked against the compiled adapter descriptor | catalog states the required implementation contract; code either matches or refuses |
| adapter-native package name and source | supply catalog recipe | provider-specific knowledge stops at the adapter edge |
| version policy, constraints, exclusions | supply catalog recipe | update policy, not a resolved or installed fact |
| exact tested evidence | supply catalog recipe evidence | signed catalog knowledge; local files never claim verification |
| exact desired closure, resolution provenance, required base lock identities | `software.lock.yaml` | portable resolution snapshot; says nothing about actual installation |
| per-installation observed closure and relation | Phase B installation receipt | historical proof; never inferred from desired state |
| current shared acquisition, lifecycle, claims, and prepared operations | root-wide Phase B software state | one concurrency/removal authority across base and experiment receipts |

The lock snapshots every immutable resolution input that applies. Catalog
provenance records a catalog snapshot when one participated; experiment
provenance records the exact experiment definition when one authorized fresh
software. They are independent because an experiment may use both. That is
history, not a second live source of policy. Receipts bind the lock's semantic
digest rather than copying its selection rules.

## Management boundary and recipe curation

Presence in this catalog has one strong meaning: Temper is permitted to resolve,
install, check, and receipt that logical package after an explicit request. A
user-managed executable therefore has no placeholder package, `external`
method, or empty recipe here. Its detection and compatibility belong to the
later qualification/harness profile; its selected integration belongs to the
manifest. This keeps permission to render configuration separate from
permission to mutate someone else's tool installation.

The initial inventory is finite:

| Package | Ownership | Candidate recipe status |
|---|---|---|
| `uv` | Temper-managed shared bootstrap tool | `system-package`/Homebrew on macOS; lock and receipt the exact formula closure |
| `hf` | Temper-managed shared model-source tool | `system-package`/Homebrew on macOS; only revision-pinned operations are eligible |
| `llama-swap` | Temper-managed Field Kit base | isolated `release-artifact` selected on macOS Apple Silicon; exact reviewed recipe identity and real lifecycle pass, shipping catalog artifact pending |
| `llama-cpp` | Temper-managed Field Kit base | isolated `release-artifact` selected on macOS Apple Silicon after Metal/runtime and compatibility screening; exact reviewed recipe identity and real lifecycle pass, shipping catalog artifact pending |
| `rapid-mlx` | Temper-managed, optional and non-default | isolated `python-environment`/`uv`; lock the exact interpreter and MLX closure |
| `mlx-dspark` | Temper-managed, optional and non-default | isolated `python-environment`/`uv`; lock the exact interpreter and Python/MLX closure |
| Pi | user-managed harness | absent from this catalog; render selected config and report observed compatibility only |

The dated [Field Kit base method
review](../qualification/field-kit-base-method-review-2026-08-24.md) rejects
both Homebrew application variants at the exact-install gate. The two isolated
release artifacts passed the bounded model-backed runtime/router gate, so the
review selects `release-artifact` with exact catalog-reviewed releases. It does
not add tested evidence. The release adapter is hermetically executable and
its real scratch install/check/remove/second-run gate passes. Publishing the
exact recipes now waits on the authenticated production catalog artifact, not
on another adapter gate.

Recipe curation prefers an isolated verified upstream artifact for a native
application and an isolated exact environment for a language application.
`system-package` is admitted for a bootstrap/environment manager, a genuine
system-wide dependency, software available only through that channel, or a
distribution shown by review to be materially more maintainable. Building an
exact source revision is the last resort. This is publication policy: the
resolver never walks the list, discovers whatever happens to be installed, or
changes method automatically.

`hf` the executable and llama.cpp's `-hf` flag are unrelated ownership
surfaces. The former is an acceptable cataloged Homebrew tool; when Temper uses
it to fetch artifacts, it may act only on an exact revision already owned by
`manifest.lock.yaml`. The latter asks llama-server to resolve a moving
repository/quant shortcut at runtime and is forbidden. The native M1
resolver/fetcher currently uses Temper's Go Hugging Face client, so it does not
discover or require an ambient `hf` command. Any later workflow that invokes
the cataloged CLI must pass its absolute observed path, suppress update and
telemetry behavior, and preserve the existing exact-revision and artifact-hash
checks.

The common adapter values and lock graph are intentionally language-neutral.
If a future concrete Temper-managed Node application appears, its reviewed
method can model Node and every package-manager artifact as ordinary exact
closure units behind a new adapter. No Node-specific fields, manager, or
ambient-runtime assumptions are added until that package exists.

## Independent catalog lifecycle

One Temper binary supports many catalog snapshots. The binary contains schema
validators, compiled adapters and protocol versions, a catalog-signing trust
root, and an embedded bootstrap snapshot. It is rebuilt only when code,
supported schemas/protocols/adapters, or trust roots change—not when package
versions or evidence change.

Catalog publication produces three distinct artifacts:

1. an immutable catalog snapshot identified by SHA-256, carrying a monotonic
   publisher `sequence` and human `published_at`;
2. a detached signature over its exact bytes;
3. a small signed channel record mapping a channel such as `stable` to
   `(schema, sequence, sha256, locator)`.

The detached signature envelope is strict YAML with one document and no
unknown fields:

```yaml
schema: temper-signature/v1
key_id: temper-catalog-2026-01
algorithm: ed25519
signature: <standard-base64 Ed25519 signature over the exact artifact bytes>
```

The signed channel record is also strict YAML with one document:

```yaml
schema: temper-software-channel/v1
channel: stable
catalog:
  schema: temper-software-supply/v1
  sequence: <monotonic publisher sequence>
  sha256: <64 lowercase hexadecimal characters>
  locator: <opaque source-reader locator>
```

v1 accepts only Ed25519, standard padded base64, and a key ID present in the
binary's explicit catalog trust root. It verifies the signature over the exact
artifact bytes before parsing those bytes. A locator is data passed to the
configured catalog source reader; it is never a command, shell fragment, or
provider guessed from machine state. Tests inject their own trust roots and
sources. Production signing keys and the embedded bootstrap snapshot are
release inputs, not values invented by this design or fetched implicitly.

The production HTTPS source has one explicit transport convention. Its
configured channel root is an absolute HTTPS directory URL; channel `stable`
reads `stable/channel.yaml` and `stable/channel.signature.yaml` below that
root. The signed catalog `locator` is an absolute HTTPS publication-directory
URL containing `catalog.yaml` and `catalog.signature.yaml`. Roots and
redirects forbid credentials, query strings, fragments, non-HTTPS schemes,
and encoded directory paths. Each read is context-bound, permits no more than
five HTTPS redirects, and has no retry, cache, credential discovery, or host
fallback. Channel bytes are capped at 64 KiB, detached signatures at 4 KiB,
and catalog bytes at 8 MiB before cryptographic or schema verification. A
failed data read prevents its signature read; a failed signature read prevents
the updater from reading or committing anything else.

The active catalog store under an explicit Temper data root is:

```text
software/catalog/
  active
  snapshots/<catalog-sha256>/catalog.yaml
  snapshots/<catalog-sha256>/catalog.signature.yaml
```

`active` is a regular file containing exactly the selected digest and one
newline. Snapshot directories are immutable and contain exactly the catalog
and its detached signature as regular files. The updater verifies the signed
channel, signed catalog, exact channel-to-catalog digest/schema/sequence join,
catalog invariants, and every declared compiled adapter capability in memory.
Only then may it stage an immutable snapshot. The single behavioral commit is
an atomic replacement of `active`, conditional on its originally observed
bytes still being current. A failed or concurrent update leaves the previous
pointer unchanged; an unreferenced verified snapshot may remain as a harmless
cache entry. Dry-run stops before creating any path.

The active sequence may increase, or remain unchanged only when the digest is
also unchanged. A lower sequence is a rollback refusal; the same sequence with
a different digest is an equivocation refusal. The active snapshot and its
stored signature are reverified before they establish this comparison point.

Catalog update is explicit; there is no background updater. The update effect
downloads or accepts a supplied snapshot, verifies signature, exact
digest, schema, sequence, compiled capabilities and all catalog invariants,
stores it under its digest, then atomically moves one active-catalog pointer.
Failure leaves the previous pointer unchanged. Dry-run may verify a small
snapshot in memory but writes no cache or stage.

Updating that pointer never resolves software, rewrites
`software.lock.yaml`, installs packages, or changes an installation receipt.
Old snapshots referenced by a lock, receipt or Field Kit packet remain
available by digest. Field Kit can carry a newer signed snapshot beside the
single binary and stays offline-capable; the embedded snapshot is the
standalone fallback.

A catalog may use only schemas, methods, version schemes, adapter protocols,
gates and concrete adapters implemented by the binary. New catalog data does
not require a binary; a new adapter or protocol does. Unsupported capability is
a refusal before resolution or installation.

## Three identities, not one provider string

1. **Package** — the Temper-managed thing (`uv`, `hf`, `llama-swap`,
   `llama-cpp`, `rapid-mlx`, `mlx-dspark`). User-managed harnesses such as Pi
   are outside this identity set.
2. **Method** — the portable strategy (`system-package`,
   `python-environment`, `release-artifact`, `source-revision`).
3. **Adapter** — the concrete implementation for a target (`homebrew`, `uv`,
   an upstream-release client; later `apt`, `dnf`, `pacman`, `winget`, etc.).

An adapter-native **recipe** joins those identities to a provider package name,
version scheme, source, constraints, and gates. Method is explicit intent.
Adapter selection is deterministic target resolution within that method.

For example, `system-package` may resolve to `homebrew` on `darwin/arm64` and
to `apt` on an explicitly supported Ubuntu target later. That is not a
fallback. Changing `rapid-mlx` from `system-package` to
`python-environment`/`uv` *is* a method change and always requires an explicit
selection. Discovery of an installed package manager never changes either.
That example demonstrates method safety; it does not make `rapid-mlx` the
primary runtime or choose a shipping method for `llama.cpp`.

## `temper-software-supply/v1`

Catalog snapshots are published independently of Temper binaries. The notation
below is schematic: angle-bracket values are not seed data and do not invent
unreviewed versions. Its Homebrew records demonstrate the shared-adapter shape;
they are candidate variants, not selected Temper application recipes.

```yaml
schema: temper-software-supply/v1
sequence: <monotonic publisher sequence>
published_at: <RFC 3339 timestamp>

methods:
  system-package:
    description: package managed in the target's shared system prefix
  python-environment:
    description: package managed in a Temper-owned Python environment
  release-artifact:
    description: verified upstream release artifact under a Temper root
  source-revision:
    description: build from an exact source revision under a Temper root

adapters:
  homebrew:
    method: system-package
    protocol: temper-installer-adapter/v1
    effect_model: shared
  uv:
    method: python-environment
    protocol: temper-installer-adapter/v1
    effect_model: isolated
  upstream-release:
    method: release-artifact
    protocol: temper-installer-adapter/v1
    effect_model: isolated

target_bindings:
  - method: system-package
    target:
      os: darwin
      arch: arm64
    adapter: homebrew
  - method: python-environment
    target:
      os: darwin
      arch: arm64
    adapter: uv
  - method: release-artifact
    target:
      os: darwin
      arch: arm64
    adapter: upstream-release

packages:
  uv:
    description: shared Python environment manager
    recipes:
      homebrew:
        method: system-package
        recipe_revision: <recipe revision>
        source:
          kind: homebrew-formula
          tap: homebrew/core
          formula: uv
        version_scheme: semver
        selection:
          policy: latest
          minimum_compatible: <reviewed lower bound>
        dependencies: []
        exclude: [<reviewed known-bad version, when any>]
        gates: [<bootstrap-tool gate id>]
        tested:
          - root_version: <exact version>
            closure_digest: <sha256>
            target:
              os: darwin
              arch: arm64
            evidence: <stable Results or release evidence id>

  hf:
    description: shared Hugging Face source CLI
    recipes:
      homebrew:
        method: system-package
        recipe_revision: <recipe revision>
        source:
          kind: homebrew-formula
          tap: homebrew/core
          formula: hf
        version_scheme: semver
        selection:
          policy: latest
          minimum_compatible: <reviewed lower bound>
        dependencies: []
        exclude: [<reviewed known-bad version, when any>]
        gates: [<revision-pinned-download gate id>]
        tested:
          - root_version: <exact version>
            closure_digest: <sha256>
            target:
              os: darwin
              arch: arm64
            evidence: <stable Results or release evidence id>

  llama-swap:
    description: local model router
    recipes:
      upstream-release:
        method: release-artifact
        recipe_revision: <reviewed release recipe revision>
        source:
          kind: release-archive
          name: llama-swap
          repository: <stable upstream repository identity>
          revision: <exact source commit>
          artifacts:
            - target: {os: darwin, arch: arm64}
              locator: <exact HTTPS release asset locator>
              sha256: <sha256>
              size: <exact compressed bytes>
              unpacked_size: <sum of regular-file bytes>
              installed_entries: <exact file/directory/symlink count>
              format: tar.gz
              archive_root: .
        version_scheme: opaque
        selection:
          policy: exact
          exact: <catalog-reviewed release tag>
        dependencies: []
        exclude: []
        gates: [<router gate id>]
        tested: []

  llama-cpp:
    description: primary llama.cpp runtime
    recipes:
      upstream-release:
        method: release-artifact
        recipe_revision: <reviewed release recipe revision>
        source:
          kind: release-archive
          name: llama-cpp
          repository: <stable upstream repository identity>
          revision: <exact source commit>
          artifacts:
            - target: {os: darwin, arch: arm64}
              locator: <exact HTTPS release asset locator>
              sha256: <sha256>
              size: <exact compressed bytes>
              unpacked_size: <sum of regular-file bytes>
              installed_entries: <exact file/directory/symlink count>
              format: tar.gz
              archive_root: <exact top-level archive directory>
        version_scheme: opaque
        selection:
          policy: exact
          exact: <catalog-reviewed build tag>
        dependencies: []
        exclude: []
        gates: [<runtime qualification gate id>]
        tested: []

  mlx:
    description: MLX runtime dependency shared by reviewed Python packages
    recipes:
      uv:
        method: python-environment
        recipe_revision: <recipe revision>
        source:
          kind: python-index
          index: <index identity>
          distribution: mlx
        version_scheme: pep440
        selection:
          policy: range
          constraint: <reviewed MLX constraint>
        dependencies:
          - package: cpython
            constraint: <reviewed compatible CPython constraint>
        exclude: [<reviewed known-bad version, when any>]
        gates: [<dependency gate id>]
        tested:
          - root_version: <exact version>
            closure_digest: <sha256>
            target:
              os: darwin
              arch: arm64
            evidence: <stable Results or release evidence id>

  cpython:
    description: uv-managed Python execution runtime
    recipes:
      uv:
        method: python-environment
        recipe_revision: <recipe revision>
        source:
          kind: python-runtime
          implementation: cpython
        version_scheme: pep440
        selection:
          policy: range
          constraint: <reviewed globally supported CPython range>
        dependencies: []
        exclude: [<reviewed known-bad interpreter build, when any>]
        gates: [<interpreter gate id>]
        tested:
          - root_version: <exact CPython version>
            closure_digest: <sha256>
            target:
              os: darwin
              arch: arm64
            evidence: <stable Results or release evidence id>

  rapid-mlx:
    description: supported non-default MLX model runtime
    recipes:
      uv:
        method: python-environment
        recipe_revision: <recipe revision>
        source:
          kind: python-index
          index: <index identity>
          distribution: <distribution name>
        version_scheme: pep440
        selection:
          policy: range
          constraint: <reviewed rapid-mlx constraint>
        dependencies:
          - package: cpython
            constraint: <reviewed compatible CPython constraint>
          - package: mlx
            constraint: <reviewed MLX constraint>
        exclude: []
        gates: [<gate id>]
        tested:
          - root_version: <exact version>
            closure_digest: <sha256>
            target:
              os: darwin
              arch: arm64
            evidence: <stable Results or release evidence id>

  mlx-dspark:
    description: supported non-default MLX speculative runtime
    recipes:
      uv:
        method: python-environment
        recipe_revision: <recipe revision>
        source:
          kind: python-index
          index: <index identity>
          distribution: <distribution name>
        version_scheme: pep440
        selection:
          policy: range
          constraint: <reviewed mlx-dspark constraint>
        dependencies:
          - package: cpython
            constraint: <reviewed compatible CPython constraint>
          - package: mlx
            constraint: <reviewed MLX constraint>
        exclude: []
        gates: [<gate id>]
        tested:
          - root_version: <exact version>
            closure_digest: <sha256>
            target:
              os: darwin
              arch: arm64
            evidence: <stable Results or release evidence id>
```

### Catalog invariants

- IDs are lowercase stable tokens. Display names and descriptions are mutable;
  IDs are not.
- Every adapter references one existing method. Every recipe key references one
  existing adapter and repeats that adapter's method only as an enforced join:
  a mismatch is invalid, never precedence.
- A catalog snapshot has at most one canonical `target_binding` for a
  `(method, target selector)`. Alternative adapters are separate, explicitly
  selected variants, not additional defaults. Two selectors that match the
  same normalized target/method are invalid even if they name the same adapter.
- Target selectors use portable facts: `os`, `arch`, and optional
  `distribution`/`distribution_version`. v1 ships only a `darwin/arm64`
  binding, but the schema does not make `darwin` or Homebrew universal.
- A recipe's `source` is a strict adapter-owned tagged shape. There is no shell
  command field, generic option bag, or executable catalog hook.
- A `release-archive` source belongs only to the compiled `upstream-release`
  adapter. It records the native name, repository identity, exact source
  revision, and a non-overlapping set of target assets. Each asset freezes an
  HTTPS locator, compressed and unpacked sizes, installed-entry count,
  lowercase SHA-256, `tar.gz` format, and the exact safe archive root. Its
  version policy is always `exact`: catalog publication adopts a newer release
  after review; installation never asks an upstream service what is latest.
- A `python-runtime` source is an adapter-owned logical dependency, not a
  package-index distribution. v1 accepts only `implementation: cpython` behind
  `uv`. Each Python application recipe constrains that logical dependency just
  like `mlx`; provider resolution must return one exact reachable interpreter
  unit with its immutable artifact and build revision. Interpreter ABI/wheel
  compatibility is verified while constructing that exact closure, not inferred
  later from ambient Python.
- `version_scheme` determines parsing and comparison. SemVer is never assumed:
  v1 recognizes `semver`, `pep440`, `git-revision`, and `opaque`; an adapter
  must implement the declared scheme or refuse the recipe. `opaque` supports
  exact selection or a provider-designated current candidate, never a locally
  invented range/minimum ordering.
- Ordered v1 constraints are an explicit comma-separated conjunction of
  `==`, `!=`, `<`, `<=`, `>`, and `>=`; PEP 440 also supports `~=`. Temper
  refuses unimplemented shorthand such as caret ranges, wildcard matching, or
  disjunction instead of guessing its meaning. Moving `latest`/range policies
  ignore prereleases unless a policy boundary explicitly names one; exact
  policy may select one. Pinned maintained Go libraries own SemVer and PEP 440
  parsing, normalization, and ordering behind Temper's version-package
  boundary; their types never enter catalog, policy, or orchestration code.
  Temper owns this deliberately bounded constraint grammar and the prerelease
  selection rule, so replacing a library does not expand catalog syntax
  implicitly.
- `minimum_compatible` is resolution policy backed by cited review; it is not
  proof that every later version was tested. `tested` may be empty for a
  policy-eligible recipe awaiting stable evidence; rows that do exist are exact
  evidence.
- A tested row identifies the root version *and exact resolved closure digest*
  on a target. A transitive dependency move leaves that tested set even when
  the root version is unchanged. That target must select the same adapter as
  the recipe carrying the evidence.
- Dependency constraints are expressed in the dependency recipe's native
  version scheme. Unknown packages, cycles, contradictory constraints, or a
  dependency with no recipe for the selected adapter are invalid.
- `exclude` wins over a range or `latest`. A known-bad candidate is never
  selected merely because it is newer.
- A provider change, method change, adapter change, recipe revision change, or
  target change is visible in the resolve diff.

## Adapter family contract

Installation support is a keyed variant family owned by the software-supply
package. CLI verbs never switch on OS, method, `homebrew`, or `uv`, and never
invoke their commands directly. An unknown or catalog-declared-but-unbuilt
adapter is a refusal.

The family provides narrow roles so reads and effects do not become one opaque
"provider" object:

| Role | Kind | Contract |
|---|---|---|
| descriptor | pure | adapter id, method, protocol, effect model, supported target matcher |
| resolver | read | strict recipe + target → provider candidates and exact dependency metadata |
| selector | pure shared core | candidates + recipe policy → exact normalized closure or refusal |
| inspector | read | lock selection + root → provider-neutral observed state |
| planner | pure shared core | desired closure + observation + prior receipt → complete effect plan |
| installer | side effect | execute one validated absolute plan; never resolve or change selection |
| reconciliation inspector | read | after interruption/unknown outcome, observe provider state before any retry |
| reconciliation decision | pure | desired + before/after observations → complete/continue/refuse classification |
| verifier | read | observed post-state → provider-neutral receipt evidence |
| removal planner | pure | receipt + exact observation + shared authority → preserve/release/retire plan or refusal |
| remover | side effect | execute only prepared absolute removals; never infer ownership or resolution |

The adapter translates vendor output at the edge into Temper-owned values:
`Candidate`, `ResolvedUnit`, `ObservedUnit`, `InstallPlan`, `EffectOutcome`, and
`ReceiptEvidence`. Those value shapes are designed with the executable slice;
vendor SDK/process types never appear in catalog, lock, receipt, or workflow
signatures.

The compiled descriptor and catalog adapter record must agree on adapter id,
method, protocol, and effect model. This handshake prevents a catalog from
claiming an isolated/atomic implementation while the binary contains a shared
package-manager adapter.

### Provider resolver edges

The Homebrew resolver implements one narrow read protocol for
`darwin/arm64`:

1. require an exact `macos` distribution version and map only a compiled,
   reviewed macOS major to a bottle tag;
2. read required and recommended transitive formulae with `brew deps
   --formula --full-name --topological --os=macos --arch=arm64`;
3. read the root and that exact closure together with `brew info --json=v1
   --variations --formula`;
4. accept additive JSON fields, as Homebrew's v1 contract permits, but refuse
   a missing/extra formula, ambiguous or unreachable dependency, an
   untranslated variation for the selected target, disabled or non-bottled
   formula, wrong target bottle, non-HTTPS locator, or missing lowercase
   SHA-256;
5. record the stable version, formula revision, version scheme, bottle rebuild,
   target bottle locator/hash, and direct dependency edges as one current
   provider candidate.

Both provider commands share one injected timeout budget and have no inner
retry. The production process edge invokes Homebrew directly without a shell
and disables its automatic update, analytics, prompts, and incidental GitHub
API use; the public command composition root is not wired yet. Black-box tests
use a recording runner and never invoke Homebrew or the network. The provider's
`all` bottle is the only fallback to an exact macOS bottle tag because it is an
explicit provider claim of portability.

The `upstream-release` resolver performs no network read. For `darwin/arm64`
it requires an exact `release-artifact`/`release-archive` recipe, selects the
one reviewed target asset, and copies its version, revision, locator, hash,
sizes, format, archive root, and installed-entry count into one dependency-free
isolated lock unit. The production transport performs one context-bound
download for that locked locator, permits at most five HTTPS-only redirects,
and owns no retry, cache, credentials, or release discovery. Hermetic tests
inject the archive bytes and never use the network.

Its effect member publishes
`<installation>/upstream-release/<scope>/current/payload`. It downloads into a
private stage, verifies compressed size and SHA-256 before parsing, and then
validates the complete tar manifest before extraction. Absolute/traversing or
duplicate paths, privileged modes, hard links and special files, escaping or
dangling symlinks, content outside the declared archive root, and size/entry
count drift all refuse. Files are extracted into a new immutable generation;
an adapter-private canonical marker and the verified source archive retain the
evidence needed to re-derive and re-hash the exact installed tree. One atomic
relative `current` symlink rename is the publish commit, so a failed add or
repair leaves the prior generation selected. Inspection refuses symlinked
ancestors and checks the pointer, group layout, marker, retained archive, every
payload path, type, normalized mode, size, hash, and safe link target. Removal
deletes only the exact prepared scope directory and refuses a symlink ancestor.

The uv interpreter surface is settled. A Python application recipe declares a
normal catalog dependency on the adapter-native `python-runtime`/`cpython`
package with its compatible PEP 440 constraint. Provider resolution selects an
exact uv-managed interpreter and returns it as a reachable closure unit with
version, build revision, immutable artifact locator/hash, and the application's
isolated scope. Exact wheel artifacts plus that interpreter unit and the lock's
target bind the ABI decision; ambient Python never participates.

The remaining uv work is an adapter reader/translator contract. uv's rich
workspace-metadata JSON is explicitly a preview schema, so it cannot silently
become the long-lived boundary. The implementation must use a reviewed stable
command/output surface or a deliberately owned narrow protocol, and it must
refuse when it cannot prove the complete interpreter-and-package closure.

Each installer must meet the same semantic contract:

- install the exact lock or refuse; never reinterpret `latest` at install time;
- second execution converges without duplicating or upgrading anything;
- dry-run performs no external write and creates no staging path;
- timeout/interruption is `unknown` until inspection reconciles it;
- no inner retry hidden beneath workflow retry ownership;
- no `sudo`; required privilege is a `[manual]` result;
- no uninstall of a pre-existing or unreceipted unit.

An adapter that cannot guarantee exact installation is not eligible for an
automatic method. It may report detected/manual work, but cannot pretend an
unpinned package-manager command satisfied the lock.

## `temper-software-lock/v1`

The lock is mechanically written by explicit software resolution/update or by
an explicit Labs/Field Kit experiment-lock generator. It is separate from
`manifest.lock.yaml`: executable experiment software can exist before a user
manifest, and model/patch resolution has a different writer and lifecycle.
Installation consumes the exact lock without reading a catalog. The example
below intentionally demonstrates catalog-backed experiment provenance plus
shared and isolated variants; it is not a chosen Temper package set or method
decision.

```yaml
schema: temper-software-lock/v1
provenance:
  catalog:                         # optional when experiment is present
    schema: temper-software-supply/v1
    sequence: <catalog sequence>
    sha256: <catalog file sha256>
  experiment:                      # optional for an ordinary catalog lock
    schema: <exact experiment-definition schema>
    id: <stable experiment id>
    definition_sha256: <canonical experiment-definition sha256>
requires:
  - software_lock_digest: <required base software-lock semantic sha256>
target:
  os: darwin
  arch: arm64
  distribution: macos       # optional; required where adapter binding uses it
  distribution_version: <observed version>
resolved: <date>

selections:
  uv:
    provenance: catalog
    method: system-package
    adapter: homebrew
    recipe_revision: <recipe revision>
    root_unit: homebrew:system:uv
  hf:
    provenance: catalog
    method: system-package
    adapter: homebrew
    recipe_revision: <recipe revision>
    root_unit: homebrew:system:hf
  llama-swap:
    provenance: catalog
    method: release-artifact
    adapter: upstream-release
    recipe_revision: <recipe revision>
    root_unit: upstream-release:llama-swap
  llama-cpp:
    provenance: catalog
    method: release-artifact
    adapter: upstream-release
    recipe_revision: <recipe revision>
    root_unit: upstream-release:llama-cpp
  rapid-mlx:
    provenance: experiment
    method: python-environment
    adapter: uv
    recipe_revision: <recipe revision>
    root_unit: uv:rapid-mlx:rapid-mlx

units:
  homebrew:system:uv:
    adapter: homebrew
    scope: system
    native_name: uv
    version: <exact provider-native version>
    revision: <exact provider metadata revision>
    dependencies: [<exact Homebrew closure unit ids>]
    artifacts:
      - locator: <immutable bottle/artifact locator>
        sha256: <sha256>
  homebrew:system:hf:
    adapter: homebrew
    scope: system
    native_name: hf
    version: <exact provider-native version>
    revision: <exact provider metadata revision>
    dependencies: [<exact Homebrew closure unit ids>]
    artifacts:
      - locator: <immutable bottle/artifact locator>
        sha256: <sha256>
  upstream-release:llama-swap:
    adapter: upstream-release
    scope: llama-swap
    native_name: llama-swap
    version: <exact catalog-reviewed release tag>
    revision: <exact source commit>
    dependencies: []
    artifacts:
      - locator: <exact HTTPS release asset locator>
        sha256: <sha256>
        size: <exact compressed bytes>
        unpacked_size: <sum of regular-file bytes>
        installed_entries: <exact file/directory/symlink count>
        format: tar.gz
        archive_root: .
  upstream-release:llama-cpp:
    adapter: upstream-release
    scope: llama-cpp
    native_name: llama-cpp
    version: <exact catalog-reviewed build tag>
    revision: <exact source commit>
    dependencies: []
    artifacts:
      - locator: <exact HTTPS release asset locator>
        sha256: <sha256>
        size: <exact compressed bytes>
        unpacked_size: <sum of regular-file bytes>
        installed_entries: <exact file/directory/symlink count>
        format: tar.gz
        archive_root: <exact top-level archive directory>
  uv:rapid-mlx:rapid-mlx:
    adapter: uv
    scope: rapid-mlx
    native_name: <distribution name>
    version: <exact PEP 440 version>
    dependencies: [uv:rapid-mlx:cpython, uv:rapid-mlx:mlx]
    artifacts:
      - locator: <immutable wheel/sdist locator>
        sha256: <sha256>
  uv:rapid-mlx:cpython:
    adapter: uv
    scope: rapid-mlx
    native_name: cpython
    version: <exact PEP 440 interpreter version>
    revision: <exact uv-managed Python build revision>
    dependencies: []
    artifacts:
      - locator: <immutable interpreter artifact locator>
        sha256: <sha256>
  uv:rapid-mlx:mlx:
    adapter: uv
    scope: rapid-mlx
    native_name: mlx
    version: <exact PEP 440 version>
    dependencies: [uv:rapid-mlx:cpython]
    artifacts:
      - locator: <immutable wheel/sdist locator>
        sha256: <sha256>
```

### Lock invariants

- `provenance` contains catalog, experiment, or both. Catalog identity is
  required only when catalog policy participated. Experiment identity binds a
  stable ID, exact definition schema, and canonical definition SHA-256; an
  unreviewed experiment lock never impersonates a production catalog lock.
- Every selection declares `provenance: catalog|experiment`, and the matching
  top-level identity must exist. This makes a mixed lock unambiguous: catalog
  validation applies only to catalog selections, while fresh experimental
  selections are bound to the exact experiment definition. If an experiment
  changes any part of a catalog package's closure, that whole logical
  selection is experimental rather than partially impersonating the catalog.
- `requires` is a duplicate-free set of base software-lock semantic digests.
  It contains desired portable identities, never machine-specific paths or
  receipt hashes. Installation supplies and verifies those observed receipts.
- `target` is the exact normalized fact set used to choose adapter bindings.
  The resolver refuses an unsupported target; it never falls back to the host's
  first installed package manager.
- For catalog-backed resolution, each selected logical package exists in that
  exact catalog and its method, adapter, recipe revision, closure, and policy
  match. A direct experiment lock instead traces those selections to its exact
  experiment definition and remains explicitly experimental; generic lock
  validation still enforces the same adapter/scope/closure/hash invariants.
- `units` is the one home for the exact resolved closure. Selection rows point
  to roots; dependency edges point to units. Every unit is reachable, every
  reference resolves, and the graph is acyclic.
- A unit has one adapter and scope. Hidden cross-adapter dependencies are
  forbidden in v1; a real cross-method dependency becomes another explicit
  logical selection so its ownership and effect boundary are visible.
- All units reachable from one root use its selected adapter. Shared units may
  be referenced by several roots but appear once and must satisfy every
  constraint.
- `version` is provider-native text validated by the recipe's scheme.
  `revision` is present when provider metadata itself is revisioned.
- Every installable artifact has an immutable locator plus SHA-256. An archive
  additionally carries the exact compressed/unpacked sizes, installed-entry
  count, format, and extraction root required by an installer that consumes
  only the lock. A source build may instead pin an exact source revision, but
  its receipt must hash the produced files. If an adapter can prove neither
  immutable input nor exact installed output, resolution refuses it.
- The lock stores no selection policy, tested flag, installed path, ownership,
  or pre-existing state. Those belong to catalog comparison or the receipt.
- The semantic digest covers `schema`, all provenance, sorted base
  requirements, target, selections, and units in canonical key order. It
  excludes `resolved`. Phase B receipts and Field Kit packets bind this digest.
- A root closure digest covers its selection identity, root unit id, and the
  complete reachable unit subgraph serialized as canonical JSON with sorted
  map keys and dependency lists. It excludes `resolved`. This is the digest
  compared with a catalog `tested` row.

## Resolution transaction

The shipping catalog resolver follows this transaction:

1. Parse and validate catalog, optional existing catalog-backed lock, requested logical
   packages/methods, and normalized target facts.
2. Resolve the one catalog-declared adapter binding per requested method and
   target. Refuse ambiguity, missing bindings, unknown adapter implementations,
   or descriptor mismatch before upstream reads.
3. Each resolver adapter reads provider-native candidates and dependency
   metadata with a timeout. It does not install.
4. Pure selection applies version scheme, minimum/range/exact policy,
   transitive constraints, and exclusions; it produces the complete normalized
   closure. Unsupported comparison is a refusal, not lexical ordering.
5. Validate reachability, acyclicity, exact artifacts/revisions, and catalog
   references; compute the semantic digest and tested-set comparison.
6. Stage the whole candidate lock, re-check the original bytes/absence for a
   concurrent writer, and atomically rename once. `--dry-run` stops before any
   temporary file or directory is created.

Existing selections never move during a fill-missing resolve. An explicit
software update may move them, but prints method/adapter/recipe/root/closure
changes separately. A fill-missing resolve also refuses when the existing lock
cites a different catalog digest/sequence or normalized target; mixing resolutions
from two catalog policies or machines would make the lock uninterpretable.
Resolution never installs, activates a service, downloads large model weights,
or touches the live legacy stack.

An experiment generator is a separate composition over the same compiled
adapter readers and generic lock validator. It may resolve catalog packages,
fresh exact provider revisions, or both. It marks each selection with the
identity that authorized it and records the immutable experiment definition
and every catalog snapshot that participated. It writes one complete lock
before installation; it cannot ask the installer to resolve or fill missing
units.

## Tested status is derived

For each selected root, compare `(recipe revision, target, root version,
closure digest)` against the recipe's signed catalog evidence:

- **exact-tested** — exact tuple exists;
- **policy-eligible, untested** — current policy admits it, exact tuple absent;
- **known-bad** — excluded by the relevant catalog snapshot;
- **outside-policy** — does not satisfy the current recipe.

The evidence target is a selector and must match the lock's exact normalized
target; the exact closure digest still binds target-specific artifacts and
dependencies. An explicit exclusion wins over retained historical tested
evidence, allowing a later catalog to mark a formerly tested closure known-bad.

No status is written into either lock or receipt. A later signed catalog may
classify the same frozen lock differently because reviewed knowledge changed;
the exact lock itself remains unchanged and auditable. `check` eventually
reports both historical status under the lock-cited snapshot and current status
under the explicitly active snapshot.

## First executable acceptance fixtures

The approved first offline slice uses synthetic catalog metadata and fake
adapters to prove behavior. Product-shaped IDs in a fixture are test data, not
reviewed package-method selections:

1. `latest` + compatibility floor selects the newest eligible candidate.
2. guarded rolling excludes a newer known-bad candidate.
3. PEP 440 resolution pins the explicitly requested, non-default `rapid-mlx`,
   its constrained exact `mlx` unit, and its exact uv-managed `cpython`
   interpreter artifact; an ambient or incompatible interpreter is never used.
4. opaque and Git revisions are never compared as SemVer.
5. `darwin/arm64 + system-package` selects the catalog's Homebrew adapter;
   a fake second-OS target selects its system adapter with no workflow change.
6. changing `system-package` to `python-environment` cannot happen as fallback.
7. missing, ambiguous, unknown, and declared-but-unbuilt adapters refuse before
   upstream reads or writes.
8. dependency conflict, cycle, orphan unit, hash gap, and descriptor/catalog
   mismatch are rejected.
9. semantic digest is deterministic across map order and ignores only the
   human `resolved` date.
10. dry-run writes nothing; a concurrent lock change refuses; a clean second
    resolution preserves the original bytes and date.

Live Homebrew/`uv` execution, network reads, installation, and Field Kit runs
are later announced, on-demand gates. None is authorized by this document.

The foundation implementation covers deterministic target-adapter selection,
declared-but-unbuilt and descriptor mismatch refusals, candidate/version
selection, structural closure/hash validation, canonical digests, and the
resolution transaction from fixtures 1–10. The signed channel and catalog
store transaction is also executable with injected hermetic keys and sources.
The Homebrew provider protocol/translator and controlled non-shell process edge
are executable. The selected `upstream-release` resolver, production HTTPS
reader, and isolated effect member are executable and hermetically exercised.
The bounded production catalog HTTPS source and the real release-adapter
scratch gate are complete. Production trust/bootstrap bytes, the channel root,
public catalog-command wiring, and the reviewed uv surface remain deliberate
release-facing steps.
