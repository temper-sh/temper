# Field Kit promotion and runtime boundary

Status: owner runtime revision accepted 2026-08-27.

Field Kit is a reviewed content boundary in Temper's supply chain. It is not a
second executable. Labs owns editable research, Field Kit retains immutable
promoted packages, and Temper embeds one reviewed snapshot in each release and
owns every runtime decision and effect.

```text
Labs                     Field Kit                    Temper release/runtime
editable evidence  --->  immutable promotion  --->  embedded verified snapshot
promotion candidates     packages and history       consent, effects, evidence
```

Normal users install or build only `temper`. Temper never reads a moving Labs
checkout or fetches a moving Field Kit catalog at runtime. An explicit
`--catalog` path is a review/development override and receives the same strict
validation as the embedded snapshot.

## Ownership

| Owner | Owns | Must not own |
|---|---|---|
| Labs | editable experiment definitions, source evidence, promotion review, returned-evidence interpretation, product-promotion decisions | a runtime dependency, user consent, or silent product changes |
| Field Kit | canonical immutable package/catalog bytes, provenance, hard applicability and advisory relevance declarations, cost/data/consent inputs, stage and protocol identities, evidence requirements, invalidation, pause, retirement, and operator policy | an executable, machine effects, process lifecycle, session mutation, or a moving runtime feed |
| Temper | embedded-snapshot verification, machine facts, applicability evaluation, exact disclosure, explicit consent, resumable sessions, package materialization, all installation/model/config effects, live protocol implementations, safety stops, evidence, reports, and cleanup | editing Labs research, widening promoted bounds, automatic evidence submission, or turning a run into a recommendation |

The Temper binary identity covers the embedded Field Kit bytes. Release
signing therefore gives users one artifact and one supply-chain identity to
verify. Any content change that affects execution creates a new package
revision and a new Temper release snapshot; the runtime accepts only protocol
identities implemented and reviewed in that release.

## Two independent promotion gates

1. **Experiment/baseline promotion: Labs → Field Kit.** Review establishes a
   useful, bounded procedure and freezes exact inputs, applicability, costs,
   effects, consent, execution declarations, evidence conditions, and
   limitations. Promotion does not validate the hypothesis or recommend the
   subject.
2. **Product promotion: Labs → Temper/Results.** Later evidence review may
   accept a qualified profile and a public explanation. A Field Kit run never
   performs this transition.

A baseline freezes an exact prior witness for safe reproduction. An experiment
asks a separately reviewed bounded decision question. Neither inherits consent
from the other.

## One editable home, immutable revisions

Labs is the only editable experiment home. Promotion creates an immutable,
content-identified Field Kit revision. Any change that can affect
applicability, consent, execution, measurement, or interpretation creates a
new revision, including changes to:

- prompt, question, inputs, candidates, parameter bounds, or stop rules;
- machine buckets, hard predicates, or advisory relevance;
- costs, destinations, writes, privacy, safety, or renewed-consent thresholds;
- stage order, Temper protocol identity, measurement, or evidence schema; and
- interruption, keep/restore, cleanup, invalidation, or retirement policy.

Old revisions remain verifiable. A catalog may activate, pause, retire, or
supersede them, but it cannot rewrite their bytes. Temper refuses new sessions
for inactive revisions.

## Required promoted content

Promotion rejects a package unless it declares at least:

- immutable identity, revision, origin, content hashes, and exact external
  inputs;
- a precise purpose and evidence boundary;
- machine-readable hard applicability predicates and separately labeled
  advisory relevance;
- fixed runtime plus setup range, network bytes, temporary/retained disk,
  memory pressure, idle need, service disruption, and paid-provider exposure;
- user choices, read/write/network boundaries, local-output policy, and
  renewed-consent conditions;
- exact ordered stages and a Temper-owned protocol identity for live work;
- evidence/result shape, required conditions, sensitivity, and explicit export
  policy;
- interruption, resume, keep/restore, and marker-guarded cleanup behavior;
- hermetic metadata/hash/refusal coverage; and
- invalidation, pause, and retirement triggers.

An experiment gathering the first witness for a machine class may be promoted
when its mechanics and safety envelope pass review and the missing evidence is
explicit. That is procedure qualification, not a positive result.

## Temper runtime contract

The public surface is `temper field-kit`. For a baseline it:

1. verifies the embedded catalog, package hashes, referenced material, and
   release-supported protocol identity;
2. detects canonical non-identifying machine facts and applies hard predicates
   deterministically;
3. renders the exact purpose, evidence, cost, data, effect, and cleanup
   disclosure without mutation;
4. requires exact disclosure bytes plus `--consent yes` for one active
   `ID@REVISION`, a declared outcome, a new dedicated root, and external
   session path;
5. binds the executing Temper bytes, catalog/package/material hashes, machine
   facts, software lock, ownership marker, outcome, and consent time into a
   resumable canonical session;
6. materializes the embedded package and machine facts into the dedicated root
   and advances only the first pending declared stage;
7. invokes its own public install/fetch/apply/check/bind/probe/remove surfaces
   and runs only an exact protocol implemented in the binary;
8. retains failed output without advancing, records successful evidence
   atomically, and resumes by reconciling the same desired state; and
9. writes a local report and, for restore, removes only a root with the exact
   session-bound marker after explicit confirmation.

Nothing is uploaded automatically. Reports retain structured outcomes,
identities, hashes, timings, and measurements, not generated model content.
Crossing promoted bytes, destinations, service scope, cost, safety, or outcome
requires renewed consent.

`temper probe serve` is the narrow loopback foreground-process primitive used
by reviewed live protocols. It verifies the exact software lock/receipt and
rendered generation before launch, uses isolated receipted binaries, owns the
process group, and supports a no-effect admission dry run. It is not the
production service lifecycle and does not choose an experiment.

`temper-field-kit-binding/v1` remains the pure Temper-material identity: exact
Temper bytes, canonical machine facts, ordered software lock/receipt
identities, manifest lock, and rendered generation. The Temper-owned Field Kit
session adds package, consent, stage, protocol, outcome, and report identity.

## Release compilation

Field Kit content is copied into `internal/fieldkit/baseline/builtin` during
release work and compiled with `go:embed`. Release checks must:

- validate both the source catalog and embedded snapshot;
- compare catalog, package, and referenced material byte for byte;
- refuse an active package whose protocol identity is not implemented;
- run hermetic session, interruption, tamper, cleanup, and protocol-unit tests;
- record catalog/package identities in the Labs compilation record; and
- keep live downloads and scratch runs behind explicit machine-owner consent.

Revision 1's Python runner is retained only inside its immutable retired
package for audit. It is not a fallback. The active revision uses the
Temper-owned Go protocol, so neither Python nor a Field Kit binary is part of
the user dependency closure.
