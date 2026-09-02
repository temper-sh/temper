# `temper software install` — exact layered software installations

Status: approved installed-base surface, revised 2026-09-02. Software-lock
provenance, the pure installation/claim planner, strict canonical installation
receipt/root-state
documents and stores, and internal keyed-adapter effect orchestration are
executable. The read-only check analyzer/reader and provenance-guided removal
planner, retiring-authority state machine, receipt release, adapter effect
orchestration, and recovery path are executable as well. The public verbs now
detect the exact macOS host target and compose two concrete isolated members:
`upstream-release` for reviewed release archives and `uv` for exact managed-
Python/wheel closures. Locks naming another installation adapter refuse
without fallback. Their hermetic scratch gates cover dry-run purity, exact
check, failed replacement, drift repair, and clean repeated install/remove. The canonical
Temper-to-Field-Kit material identity binding described below is executable as
a pure schema/builder. Field Kit owns its question-package sessions around that
inner Temper material identity; see
[`../design/field-kit-question-boundary.md`](../design/field-kit-question-boundary.md).

`software install` reconciles one already-resolved `software.lock.yaml` into
one named installation below an explicit Temper root. It never resolves
versions, changes methods, chooses packages, reads an active catalog, or
touches the live legacy stack. A valid exact experiment lock is installable
even when its software has never appeared in Temper's published catalog.

Labs or Field Kit may freeze a lock while preparing an experiment. An
explicit experiment workflow may instead generate one at run time when its
declared network/provider boundary permits that read. Generation and
installation remain separate verbs: once written, the exact lock is immutable
input to `software install`.

Each lock selection says whether the catalog or the experiment definition
authorized it. A mixed lock can therefore reuse catalog-pinned base tools and
carry a fresh experimental runtime without exempting the catalog selections
from catalog validation. If an experiment changes a catalog package's closure,
that complete selection is marked experimental.

## Invocation, identity, and paths

```text
temper software install --root PATH --installation ID
  [--lock software.lock.yaml]
  [--require-receipt PATH]...
  [--dry-run]
```

`--root` is one explicit Temper control root, not an individual environment.
`--installation` is a lowercase stable identity such as `field-kit-base` or
`llama-cpp-pr-smoke`. It allows a base and several experiments to coexist
without conflating their receipts or isolated files.

Temper derives all persistent paths; none is another user choice:

```text
<root>/software/state.yaml
<root>/software/installations/<installation>/installation-receipt.yaml
<root>/software/installations/<installation>/...
```

The root-wide state arbitrates shared provider units and in-flight operations.
Every isolated adapter location must be strictly below the named installation
directory. Shared adapters may report an absolute provider-owned location
outside it.

The compiled `upstream-release` adapter uses one scope-owned layout below that
boundary:

```text
<installation>/upstream-release/<scope>/
  current -> generations/<immutable staged generation>
  generations/<immutable staged generation>/
    payload/                 # receipt location and runnable release contents
    .temper/archive.tar.gz   # exact locked input retained for inspection
    .temper/unit.json        # canonical adapter-private identity/tree marker
```

The marker is not desired policy or removal authority and does not extend the
installation receipt/root-state contract;
inspection accepts it only when its identity matches the lock, its retained
archive still matches locked bytes, and a fresh full tree scan matches the
manifest re-derived from that archive. A new generation is fully downloaded,
validated, extracted, and synced before one atomic relative `current` symlink
rename. A pre-commit failure leaves the prior pointer unchanged. Prepared
receipt authority permits whole-scope repair; removal targets the scope
directory, refuses symlinked ancestors, and cannot reach a sibling scope.

The compiled `uv` adapter uses the parallel isolated shape:

```text
<installation>/uv/<scope>/
  current -> generations/<immutable generation>
  generations/<immutable generation>/
    environment/                     # receipt location and runnable Python environment
    .temper/artifacts/               # exact runtime archive and validly named wheels
    .temper/requirements.txt         # exact versions plus every allowed wheel hash
    .temper/unit.json                # canonical closure/artifact/tree marker
```

It accepts one uv-resolved wheel-only scope with one exact managed CPython
runtime. The same internal archive boundary used by `upstream-release` owns
bounded inspection, safe extraction, and canonical tree inventory; the
adapters retain separate receipts and publication lifecycles. Runtime
extraction rejects traversal, special files, privileged modes, duplicate paths,
and unsafe links. Installation invokes only
pip bundled in that locked runtime against the local retained wheelhouse with
hashes required, dependency resolution and indexes disabled, and no ambient
Python/package-manager settings. The environment is built at its final
unpublished path so console-script interpreter paths remain valid, then the
same atomic relative-pointer commit publishes it. Inspection re-hashes the
retained artifacts and every installed file; a failed repair leaves the old
pointer selected. Multiple uv scopes remain independent.

Each lock `requires` zero or more base software-lock semantic digests. The
caller supplies exactly one canonical receipt for every required digest with
`--require-receipt`; Temper validates its target, installation identity,
provider state, canonical SHA-256, and lock binding before planning. Extra,
missing, duplicate, or drifting requirements refuse.

A completed success begins with:

```text
RESULT software-install changed|unchanged|would-change installation=<id> packages=<n> units=<n> effects=<n> claims=<n>
```

Stable detail lines follow in package, adapter/scope, then unit order:

```text
PACKAGE <id> method=<method> adapter=<adapter> root-unit=<unit-id>
EFFECT <adapter>:<scope> install|publish-isolated|unchanged units=<n>
UNIT <unit-id> add|preserve|replace ownership=temper-added|pre-existing claim=none|add|activate|preserve
```

For a shared unit, `ownership` reports the registry's acquisition history; the
claim—not that word—authorizes continued use or eventual removal.

An adapter that needs privilege Temper cannot exercise returns a ready-to-paste
`MANUAL ...` line and exit `1`; it never invokes `sudo`. Usage refusal is exit
`2`. Another input, inspection, planning, provider, verification, concurrency,
or filesystem refusal is exit `1`. Exit `0` means the `RESULT` line is valid.
No `RESULT` line is emitted for a fatal refusal.

Dry-run performs every parse, receipt/state read, provider inspection,
comparison, and pure planning step, then stops. It creates neither the root,
state, operation, stage, claim, nor receipt and invokes no installer effect.

## Functional boundary and commit protocol

The workflow composes these units in order:

1. **Reads:** strict lock, every required base receipt, optional own receipt,
   root-wide state, and exact provider state for every adapter/scope.
2. **Pure plan:** desired lock + named installation + verified requirements +
   normalized observation + receipt/state provenance → the complete provider,
   claim, and receipt plan, or refusal.
3. **Prepare-state commit:** when a provider effect or shared-claim transition
   is needed, atomically add immutable operation intent and all provisional
   shared claims to the single root-state document.
4. **Adapter effects:** execute only prepared absolute groups. Shared effects
   are reconcilable; isolated effects stage and atomically publish a complete
   installation-owned scope.
5. **Reads:** inspect the complete post-state. A timeout or interruption is
   `unknown`, never assumed failed.
6. **Receipt commit:** only an exact post-state can atomically publish the
   installation receipt.
7. **Finalize-state commit:** activate its prepared claims and remove the
   now-redundant operation. A crash after the receipt commit is reconciled by
   this final idempotent state transition.

Planning never invokes a provider. An installer never resolves, changes the
lock, or invents a command from catalog text. Provider-native values stop at
the adapter edge. The root-state writer is the single concurrency arbiter;
per-installation files never attempt a second ownership decision.

## Fresh, repeated, and layered planning

The planner works per independently reconcilable `(adapter, scope)` group.
Dependencies are ordered before dependants and every group is known before the
first commit.

With no receipt for this installation:

- a shared unit already registered at the exact desired identity is preserved
  and gains one claim; it is not reinstalled and its acquisition history is
  not reassigned;
- an unregistered shared group may preserve exact provider units or install
  absent units, then creates registry records and prepared claims atomically;
  a present non-exact unit refuses;
- an isolated group must be wholly absent or wholly exact inside this
  installation's directory. A partial or non-exact unreceipted environment is
  not adopted or overwritten;
- exact unregistered units have `pre-existing` acquisition and units installed
  by Temper have `temper-added` acquisition.

With a receipt for the same lock digest, exact units retain their recorded
relation. A missing shared unit may be repaired only when root state proves
Temper originally added it and this installation still has a claim. A wholly
Temper-owned isolated group may be republished in full; an isolated group that
contains any pre-existing unit is never replaced. Shared identity drift is a
refusal rather than an implicit upgrade or downgrade.

A receipt for a different lock is also a first-slice refusal. The explicit
software-update workflow must define obsolete-unit removal and shared-package
migration. A different lock can coexist immediately under a different
installation ID. Unknown observation, an extra prior unit, requirement drift,
target/root/installation drift, or an adapter/scope mismatch refuses before
effects.

## `temper-software-state/v1`: operations and shared claims

One root-wide canonical document is the current authority for prepared work
and shared-provider acquisition. Keeping both in one file makes “record intent
and acquire claims” one atomic commit instead of an unsafe two-file dual write:

```yaml
schema: temper-software-state/v1
root: <absolute clean Temper root>
generation: <monotonic positive integer>
operations:
  <installation id>:
    kind: install
    software_lock_digest: <semantic sha256>
    target: <exact normalized target>
    plan_digest: <canonical plan sha256>
    started_at: <RFC 3339 UTC instant>
    claimed_by: <opaque invocation id>
    lease_expires_at: <RFC 3339 UTC instant>
    fence: <monotonic positive integer>
    groups:
      <adapter>:<scope>:
        adapter: <adapter id>
        scope: <scope id>
        effect_model: shared|isolated
        units:
          <unit id>:
            before: absent|exact|non-exact
            ownership_after: temper-added|pre-existing
            shared_claim: <shared-unit sha256, only when shared>
shared_units:
  <shared-unit sha256>:
    adapter: <adapter id>
    scope: <scope id>
    native_name: <provider-native name>
    version: <exact provider-native version>
    revision: <exact provider revision, when present>
    dependencies: [<unit ids>]
    artifacts:
      - locator: <immutable locator>
        sha256: <sha256>
    location: <absolute observed or prepared provider location>
    acquisition: temper-added|pre-existing
    lifecycle: active|retiring
    claims:
      <installation id>:
        software_lock_digest: <semantic sha256>
        unit_id: <lock unit id>
        status: prepared|active
```

The shared-unit key is SHA-256 over canonical length-safe JSON containing
`adapter`, `scope`, and `native_name`; it identifies the provider object rather
than trusting lock-local unit spelling. The record's exact version/revision/
artifact identity must agree for every claimant. A conflicting claimant
refuses before mutation.

An install operation unit carries `before`, `ownership_after`, and an optional
`shared_claim`. A remove operation unit instead carries `before`,
`ownership_before`, `location`, `remove_provider`, `retire_shared`, and an
optional `shared_claim`. The operation's plan, pre-state, ownership, and
provider action are immutable recorded intent. Only lease fields may renew.
`non-exact` pre-state is admitted only for a receipted, wholly Temper-owned
isolated install group. On restart, Temper re-inspects reality: completed
absolute work is not repeated, incomplete idempotent work may continue, and
unsafe non-exact shared state refuses. The explicit rerun is the sole retry
owner; adapters have no hidden inner retry.

State creation/update is an atomic exclusive claim. The renewable lease uses
`fence` as its fencing token; every effect rechecks it immediately before
invoking the provider, and every state completion is conditional on the same
token. A live holder makes a concurrent invocation refuse. An expired holder
may be reclaimed only after inspection and with a higher fence.

Prepared claims count as claims: another installation cannot remove or replace
their shared unit during a crash window. Shared units are normally `active`.
The serialized final release removes the last claim and changes an exact
Temper-added unit to `retiring` in the same state commit. A retiring generation
accepts no new claims and may have no claims only while its matching remove
operation exists. Observed absence permits finalization and removal of the
retiring record. A pre-existing final release preserves the provider and drops
Temper's authority record. If the final install receipt already matches, the
state transition activates claims and removes leftover intent. A failure before
prepare changes nothing; a failure after it leaves enough durable state to
reconcile and is never silently discarded.

## `temper-software-installation/v1`

The receipt is per-installation observed history, not desired policy or global
ownership authority:

```yaml
schema: temper-software-installation/v1
installation: <installation id>
software_lock_digest: <semantic sha256>
target: <exact normalized target>
root: <absolute clean Temper root>
observed_at: <RFC 3339 UTC instant>
requirements:
  - software_lock_digest: <required base lock semantic sha256>
    installation: <base installation id>
    receipt_sha256: <canonical base receipt sha256>
selections:
  <logical package>:
    provenance: catalog|experiment
    method: <method id>
    adapter: <adapter id>
    recipe_revision: <recipe revision>
    root_unit: <unit id>
units:
  <unit id>:
    adapter: <adapter id>
    scope: <scope id>
    native_name: <provider-native name>
    version: <exact provider-native version>
    revision: <exact provider revision, when present>
    dependencies: [<unit ids>]
    artifacts:
      - locator: <immutable locator>
        sha256: <sha256>
    location: <absolute observed installation path>
    ownership: temper-added|pre-existing
    shared_claim: <shared-unit sha256, only when adapter is shared>
```

Selections and unit identities exactly equal the bound lock. Requirements
exactly equal the lock's required digests and add the actual base installation
and canonical receipt identity used. `location`, `ownership`, `shared_claim`,
`root`, and `observed_at` are observed history. For isolated units, ownership
guides removal. For shared units, ownership snapshots acquisition history but
only the current root-state claim set authorizes removal.

The receipt contains no version policy, tested status, recommendation,
provider credentials, or free-form adapter data. Parsing is strict: one YAML
document, no aliases, duplicate keys, unknown fields, implicit coercion,
invalid IDs, relative/unclean locations, unreachable units, or noncanonical
bytes. Maps and dependency/artifact/requirement lists have canonical order and
the file ends with one newline. A clean second run preserves its original bytes
and `observed_at`.

## Check, removal, and packet identity

The read-only check surface is:

```text
temper software check --root PATH --installation ID
  [--lock software.lock.yaml]
  [--require-receipt PATH]...
```

It strictly reads the lock, caller-supplied required receipts, optional own
receipt, root state, and normalized provider observation. It never resolves,
installs, repairs, stages, claims, renews a lease, or writes. There is no
`--dry-run`: every check invocation already has dry-run's no-mutation
semantics.

One completed report begins:

```text
RESULT software-check exact|findings installation=<id> packages=<n> units=<n> requirements=<n> problems=<n> receipt=<sha256|none>
```

Required bases and desired units follow in stable digest and dependency order:

```text
REQUIREMENT <software-lock-sha256> exact|missing|drifted installation=<id|none> receipt=<sha256|none>
UNIT <unit-id> exact|missing|drifted|unclaimed|unreceipted adapter=<id> scope=<id> location=<absolute-path|none> ownership=temper-added|pre-existing|unknown claim=<sha256|none>
```

The unit status is the first failed layer in provider → receipt → shared-claim
order. `missing` means the provider explicitly observed absence. `drifted`
means a present provider identity/location, receipt binding, or existing claim
disagrees. `unreceipted` means provider state is exact but this installation
has no receipt. `unclaimed` means an exact receipted shared unit has no matching
active root-state claim. `exact` means every applicable layer agrees.

Independent problems follow in stable requirement, unit, code, then detail
order:

```text
PROBLEM code=<code> unit=<unit-id|none> requirement=<software-lock-sha256|none> detail=<quoted-string>
```

The first check slice defines these codes:

- `required-receipt-missing`
- `required-receipt-drift`
- `receipt-missing`
- `receipt-drift`
- `provider-missing`
- `provider-drift`
- `claim-missing`
- `claim-drift`
- `operation-prepared`

An exact provider read reporting `Present: false` is a finding; a provider read
that fails is fatal because no valid report can be completed. The desired lock
and every present receipt/state document remain strict inputs: malformed,
noncanonical, unreadable, or symlinked files refuse with no `RESULT` line.
Omitting a required receipt path is a finding; naming a path that cannot be
read is a fatal input failure. Exit `0` means `exact`, exit `1` means findings
or fatal refusal, and exit `2` means CLI usage refusal.

A matching prepared operation is reported as `operation-prepared`; check never
reclaims or finalizes it. A clean result preserves every input byte and creates
no root or stage.

The removal surface is:

```text
temper software remove --root PATH --installation ID
  [--lock software.lock.yaml]
  [--dry-run]
```

It accepts the exact lock used by the installation; it never resolves or
substitutes software. A completed result begins:

```text
RESULT software-remove changed|unchanged|would-change installation=<id> packages=<n> units=<n> effects=<n> claims=<n>
```

Stable group and dependency-ordered details follow:

```text
EFFECT <adapter>:<scope> remove|preserve units=<n>
UNIT <unit-id> remove|preserve ownership=temper-added|pre-existing claim=none|release
```

With no receipt and no prepared remove operation or remaining claim for the
installation, removal is an unchanged success. Otherwise the receipt must
exactly bind the supplied lock, installation, root, and target. Provider or
root-state identity drift refuses before prepare. `--dry-run` performs all
available reads, inspection, and planning, but creates no state, changes no
claim, calls no remover, and retains the receipt.

Removal follows one recoverable state machine:

1. Read the exact lock, optional receipt, root state, and provider state.
2. Purely derive the complete release and provider plan.
3. Atomically record immutable `kind: remove` intent and release this
   installation's shared claims. A non-final release leaves the active provider
   generation. The final Temper-added release changes it to `retiring`; the
   final pre-existing release drops Temper's authority while preserving the
   provider.
4. Invoke only prepared idempotent provider removals, checking the lease fence
   before every group.
5. Re-inspect. Every removed unit must be absent; a unit preserved for another
   claim must remain exact.
6. Conditionally remove the unchanged receipt, then finalize root state by
   deleting the operation and any successfully retired shared records.

A crash after prepare is recovered only by an explicit rerun. The operation
contains locations and provider actions, so recovery still works if the receipt
commit already succeeded. A live lease refuses a concurrent run; an expired
lease may be reclaimed after inspection with a higher fence. The second
successful run is byte-for-byte clean.

A shared provider unit is removed only for a final release when root state
proves the exact generation was Temper-added. Prepared and active claims both
block final release. No new claim may attach to `retiring`. Every pre-existing,
unproven, or identity-drifted shared unit is preserved or refused, never
deleted. For an isolated adapter/scope, provider removal occurs only when every
receipted unit in that atomic group is Temper-added and every location is
strictly below the named installation directory. If any unit in that group was
pre-existing, the whole provider group is preserved while the installation
receipt is released.

Field Kit binds a run's Temper-managed material to the canonical bytes of one
`temper-field-kit-binding/v1` document:

```yaml
schema: temper-field-kit-binding/v1
temper_binary:
  os: darwin
  arch: arm64
  sha256: <sha256 of the exact Temper executable bytes>
machine:
  schema: temper-machine-facts/v1
  target:
    os: darwin
    arch: arm64
    distribution: macos
    distribution_version: <exact macOS product version>
  hardware_model: <hw.model>
  chip: <machdep.cpu.brand_string>
  os_build: <exact macOS build>
  physical_memory_bytes: <positive integer>
  metal_device_memory_mib: <81-percent wall-model estimate>
  metal_device_memory_source: predicted-metal-81-percent
  wired_limit_mib: <live value or conservative predicted default>
  wired_limit_source: live-sysctl|predicted-macos-default
manifest_lock:
  schema: temper-lock/v1
  sha256: <sha256 of the exact validated manifest.lock.yaml bytes>
rendered_generation:
  sha256: <content-derived apply generation id>
installations:
  - installation: field-kit-base
    software_lock_digest: <software-lock semantic sha256>
    receipt_sha256: <canonical installation-receipt.yaml sha256>
    requirements: []
  - installation: <experiment id>
    software_lock_digest: <software-lock semantic sha256>
    receipt_sha256: <canonical installation-receipt.yaml sha256>
    requirements:
      - installation: field-kit-base
        software_lock_digest: <base software-lock semantic sha256>
        receipt_sha256: <base canonical receipt sha256>
        requirements: []
```

The top-level installation list is ordered input, not a set that Temper sorts.
Every installation ID is unique. A required receipt must occur earlier in that
list and its complete identity is copied beneath `requirements`; a base which
itself requires another base therefore carries that earlier identity again,
recursively. The nested copy must equal the earlier top-level identity exactly.
This represents a base plus zero or many experiment environments without
pretending they are one lock or allowing a dangling receipt hash.

The builder accepts already-read bytes and typed values and performs no
filesystem, service, provider, or network access. It validates each exact
software lock against its canonical receipt, requires every receipt to share
the machine target and isolated Temper root, verifies every receipt requirement
against an earlier supplied receipt, hashes the exact binary and manifest-lock
bytes, and preserves installation order. Parsing accepts only the canonical
YAML bytes emitted by the schema. The manifest-lock checksum is deliberately a
byte identity, while software locks use their resolution-date-independent
semantic digest and receipts use their canonical byte identity.

Field Kit consumes these identities from a promoted experiment prompt and adds
its experiment, consent, attempt, decision, observation, and report identities
in its own session envelope. It does not edit Temper's receipts or root state,
invoke provider adapters directly, or make Temper parse the moving experiment
catalog.
