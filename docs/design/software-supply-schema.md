# Exact software inputs

The [current catalog](../contracts/execution-lock.md) owns software source facts
and resolution. There is no separate software-supply catalog, generic candidate
policy, or software catalog updater. The earlier proposal is retained in Git at
`f59c281:docs/design/software-supply-schema.md`.

## Owners

| Fact | Owner |
|---|---|
| Available preset and required/tested versions | `internal/catalog` |
| Latest/exact release discovery | `adapter/upstreamrelease` |
| Exact desired software closure | `software/lockfile` |
| Install/check/remove decisions | `installplan`, `checkplan`, `removeplan` |
| Provider effects and observed installed files | `adapter/upstreamrelease`, `adapter/uv` |
| Installation observation | `software/receipt` |
| Prepared operations and shared-unit authority | `software/rootstate` |
| Signed catalog publication | `internal/catalog/distribution` |

Resolution records exact artifacts; installation never resolves a newer version
or substitutes an ambient interpreter/index. A receipt records an observation,
not future desired state or proof that nothing changed afterward.

## `temper-software-lock/v1`

A lock contains `schema`, `provenance`, `requires`, `target`, optional
`target_mode` and `resolved`, `selections`, and `units`.

`provenance.execution` identifies a `temper-execution-lock/v3` preset and its
execution digest. Independently authored exact experiments may instead provide
`provenance.experiment` with schema, ID and immutable definition SHA-256.
Selections name the matching provenance kind. Catalog provenance is rejected.
Derived catalog software locks omit `resolved`; the source catalog date is not
a resolution or installation observation.

Each selection records an installer method/adapter, recipe revision and root
unit. Each unit records adapter, installation scope, native package name,
version, optional revision, dependencies and exact artifact descriptors.
Dependencies must exist and be acyclic; units must belong to a selected closure.
The artifact descriptor includes locator and SHA-256, plus byte size and archive
inventory where the installation method requires them. Archives also declare
format, root, unpacked bytes and installed entry count.

An omitted `target_mode` requests exact target matching. `compatible` is an
explicit portable macOS ARM64 target without observed distribution/version
fields. Consuming-machine identity belongs in machine evidence. Base-installation
requirements name exact semantic software-lock digests.

Semantic and per-selection closure hashes canonicalize unordered dependencies,
selections and units. Observational dates do not change desired identity. Exact
closures can therefore be shared across different preset settings while
independently selected software versions stay separate.

## Installation boundary

The compiled adapter registry admits only known method/protocol/target contracts.
The production effect family contains isolated release-archive and exact Python
environment installers. A locked unknown adapter is refused before effects.
Generic shared-unit ownership remains part of installation planning; it is not
an automatic route to a system package manager.

Release installation uses bounded archive verification and immutable generations.
Python installation uses the locked managed interpreter, wheelhouse and exact
source dependencies. Archive path, link, mode, size and hash rules are shared
through `software/archive`; the two adapters retain separate lifecycles.

Mutations first establish root/operation authority, then perform declared effects,
then record observed receipts and finalize ownership. Check is read-only.
Second runs reconcile interrupted work and reuse verified material. Removal is
bounded by receipts and ownership, and never treats absence of evidence as
permission to delete unrelated material.

See [software install/check/remove](../contracts/software-install.md) and the
[code map](../CODE.md) for command and store boundaries. Their hermetic tests
cover interruption, drift, concurrency and failure at the real effect boundaries.
