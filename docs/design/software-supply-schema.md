# Software supply + lock — C4/C5 design

Status: **approved by owner**, 2026-08-20. The executable M2 Phase A shared
resolver now consumes this surface: strict catalog/lock parsing and validation,
normalized target selection, compiled adapter descriptor matching,
provider-neutral candidate closures, SemVer/PEP 440 policy selection, closure
invariants, canonical digests, and the dry-run/concurrency-safe atomic lock
transaction. The Homebrew candidate protocol and strict JSON translator are
now executable behind an injected runner. The uv reader, tested-status
reporting, and the public command surface remain pending. The internal signed
channel/catalog verification, immutable store, rollback and equivocation
policy, capability gate, dry-run, and active-pointer transaction are now
executable and hermetically tested. Production trust/bootstrap and transport
inputs remain release work. Later schema changes still require review.

The current Field Kit runtime path is `llama-cpp`. `rapid-mlx` remains a
supported, non-default package and a useful supply-chain fixture: it exercises
an explicit `python-environment`/`uv` method, PEP 440, and a constrained MLX
dependency whose newer releases may regress performance. Nothing selects or
installs `rapid-mlx` unless the user or a reviewed packet explicitly requests
it.

The Phase A resolver answers one narrow question:

> Given a logical package, an explicit installation method, a signed catalog snapshot,
> and target-machine facts, which exact software closure should Temper install?

It does not install that closure. Resolution is a read plus pure selection and
one lock-file commit. Installation and its receipt are the following M2 Phase B
slice.

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
| exact desired closure | `software.lock.yaml` | resolution snapshot; says nothing about actual installation |
| actual installed closure and ownership | Phase B installation receipt | observed proof; never inferred from the desired lock |

The lock snapshots catalog identity and target facts used by resolution because
it must remain interpretable after the active catalog changes. That is history,
not a second live source of policy. The receipt will bind the lock's semantic
digest rather than copying its selection rules.

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

1. **Package** — the thing Temper needs (`llama-swap`, `llama-cpp`,
   `rapid-mlx`).
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
primary runtime. The current primary runtime package is `llama-cpp` through
the declared target system-package adapter.

## `temper-software-supply/v1`

Catalog snapshots are published independently of Temper binaries. The notation
below is schematic: angle-bracket values are not seed data and do not invent
unreviewed versions.

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

target_bindings:
  - method: system-package
    target:
      os: darwin
      arch: arm64
    adapter: homebrew

packages:
  llama-swap:
    description: local model router
    recipes:
      homebrew:
        method: system-package
        recipe_revision: <recipe revision>
        source:
          kind: homebrew-formula
          tap: <tap>
          formula: <formula>
        version_scheme: semver
        selection:
          policy: latest
          minimum_compatible: <reviewed lower bound>
        dependencies: []
        exclude: []
        gates: [<gate id>]
        tested:
          - root_version: <exact version>
            closure_digest: <sha256>
            target:
              os: darwin
              arch: arm64
            evidence: <stable Results or release evidence id>

  llama-cpp:
    description: primary llama.cpp runtime
    recipes:
      homebrew:
        method: system-package
        recipe_revision: <recipe revision>
        source:
          kind: homebrew-formula
          tap: <tap>
          formula: <formula>
        version_scheme: semver
        selection:
          policy: latest
          minimum_compatible: <reviewed lower bound>
        dependencies: []
        exclude: [<reviewed known-bad version, when any>]
        gates: [<runtime qualification gate id>]
        tested:
          - root_version: <exact version>
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
  proof that every later version was tested. `tested` rows are exact evidence.
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
| remover | side effect | remove only units whose receipt proves Temper added |

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
retry. Process execution is not yet bound in the production composition root;
black-box tests use a recording runner and never invoke Homebrew or the
network. The provider's `all` bottle is the only fallback to an exact macOS
bottle tag because it is an explicit provider claim of portability.

The uv edge is deliberately gated on one remaining surface decision. An exact
Python resolution depends on interpreter implementation, version and ABI as
well as platform markers, while v1 currently gives the recipe only an index
and distribution and gives machine `target` only OS/distribution/architecture
facts. Ambient Python cannot fill that gap: it would make identical inputs
resolve differently. uv's rich workspace-metadata JSON is also explicitly a
preview schema, so parsing it cannot silently become the long-lived adapter
contract.

Before implementing uv, D16 must choose and test where interpreter policy and
identity live. The current lean is for a uv recipe to declare a compatible
Python policy, for resolution to select an exact uv-managed interpreter, and
for the lock to record that interpreter as a unit in the isolated closure.
The alternative is adding Python implementation/version/ABI to the resolution
target. Until reviewed, the adapter refuses to exist rather than emitting an
OS-only lock that looks exact but is not.

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

The lock is mechanically written by explicit software resolution/update. It is
separate from `manifest.lock.yaml`: Field Kit needs executable software before
a user manifest exists, and model/patch resolution has a different writer and
lifecycle.

```yaml
schema: temper-software-lock/v1
catalog:
  schema: temper-software-supply/v1
  sequence: <catalog sequence>
  sha256: <catalog file sha256>
target:
  os: darwin
  arch: arm64
  distribution: macos       # optional; required where adapter binding uses it
  distribution_version: <observed version>
resolved: <date>

selections:
  llama-swap:
    method: system-package
    adapter: homebrew
    recipe_revision: <recipe revision>
    root_unit: homebrew:system:llama-swap
  llama-cpp:
    method: system-package
    adapter: homebrew
    recipe_revision: <recipe revision>
    root_unit: homebrew:system:llama-cpp
  rapid-mlx:
    method: python-environment
    adapter: uv
    recipe_revision: <recipe revision>
    root_unit: uv:rapid-mlx:rapid-mlx

units:
  homebrew:system:llama-swap:
    adapter: homebrew
    scope: system
    native_name: <formula name>
    version: <exact provider-native version>
    revision: <exact provider metadata revision>
    dependencies: []
    artifacts:
      - locator: <immutable bottle/artifact locator>
        sha256: <sha256>
  homebrew:system:llama-cpp:
    adapter: homebrew
    scope: system
    native_name: <formula name>
    version: <exact provider-native version>
    revision: <exact provider metadata revision>
    dependencies: []
    artifacts:
      - locator: <immutable bottle/artifact locator>
        sha256: <sha256>
  uv:rapid-mlx:rapid-mlx:
    adapter: uv
    scope: rapid-mlx
    native_name: <distribution name>
    version: <exact PEP 440 version>
    dependencies: [uv:rapid-mlx:mlx]
    artifacts:
      - locator: <immutable wheel/sdist locator>
        sha256: <sha256>
  uv:rapid-mlx:mlx:
    adapter: uv
    scope: rapid-mlx
    native_name: mlx
    version: <exact PEP 440 version>
    dependencies: []
    artifacts:
      - locator: <immutable wheel/sdist locator>
        sha256: <sha256>
```

### Lock invariants

- `target` is the exact normalized fact set used to choose adapter bindings.
  The resolver refuses an unsupported target; it never falls back to the host's
  first installed package manager.
- Each selected logical package exists in the catalog, and its method, adapter,
  and recipe revision match the chosen catalog path.
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
- Every installable artifact has an immutable locator plus SHA-256. A source
  build may instead pin an exact source revision, but its receipt must hash the
  produced files. If an adapter can prove neither immutable input nor exact
  installed output, resolution refuses it.
- The lock stores no selection policy, tested flag, installed path, ownership,
  or pre-existing state. Those belong to catalog comparison or the receipt.
- The semantic digest covers `schema`, catalog identity, target, selections,
  and units in canonical key order. It excludes `resolved`. Phase B receipts
  and Field Kit packets bind this digest.
- A root closure digest covers its selection identity, root unit id, and the
  complete reachable unit subgraph serialized as canonical JSON with sorted
  map keys and dependency lists. It excludes `resolved`. This is the digest
  compared with a catalog `tested` row.

## Resolution transaction

1. Parse and validate catalog, optional existing lock, requested logical
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

## Tested status is derived

For each selected root, compare `(recipe revision, target, root version,
closure digest)` against the recipe's signed catalog evidence:

- **exact-tested** — exact tuple exists;
- **policy-eligible, untested** — current policy admits it, exact tuple absent;
- **known-bad** — excluded by the relevant catalog snapshot;
- **outside-policy** — does not satisfy the current recipe.

No status is written into either lock or receipt. A later signed catalog may
classify the same frozen lock differently because reviewed knowledge changed;
the exact lock itself remains unchanged and auditable. `check` eventually
reports both historical status under the lock-cited snapshot and current status
under the explicitly active snapshot.

## First executable acceptance fixtures

The approved first offline slice uses synthetic packages and fake adapters to
prove:

1. `latest` + compatibility floor selects the newest eligible candidate.
2. guarded rolling excludes a newer known-bad candidate.
3. PEP 440 resolution pins the explicitly requested, non-default `rapid-mlx`
   and its constrained exact `mlx` unit.
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
The Homebrew provider protocol/translator is executable with an injected
recording runner; production trust/bootstrap, transport/process bindings, and
the reviewed uv surface remain deliberate release-facing steps.
