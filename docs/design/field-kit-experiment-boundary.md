# Field Kit experiment boundary

Status: owner-approved product boundary, 2026-08-24. This document records
Temper's side of the contract. Labs owns the detailed promotion rules and
Field Kit owns its package, catalog, and prompt formats; those schemas are
designed in their repositories rather than imported here.

## Decision

Field Kit is a user-facing, agent-operated catalog of current
machine-dependent experiments. It is no longer a friend-only installer and it
is not another implementation of Temper.

The Field Kit root prompt uses a read-only Temper machine probe, evaluates the
promoted experiment catalog against those facts, and suggests applicable
experiments with an explanation and estimated cost. The user explicitly opts
into each experiment. An experiment prompt may then analyze behavior and adapt
parameters within its reviewed bounds while Temper performs the exact reads
and effects.

The current standalone Bash Field Kit is a behavior oracle for this rebuild,
not the destination architecture. Its useful experiment semantics are
extracted into promoted packages; its duplicated installation stack is not.
Removing or replacing that adjacent repository is a separately authorized
cross-repository step after the parity gate below.

## Three owners, two promotions

The boundary is whether a component is deciding what to try, executing a
declared trial, or accepting a conclusion:

| Owner | Owns | Does not own |
|---|---|---|
| Labs | experiment authoring, adaptive reasoning policy, promotion review, returned raw evidence, and product conclusions | product installation effects or an automatic path into release |
| Field Kit | the root discovery/consent prompt, immutable promoted experiment packages, current runnable index, per-experiment prompts, and local session reports | mutable Labs state, Temper internals, or qualification decisions |
| Temper | canonical machine facts, exact software/model/config identities, reversible installation, isolated serving and measurement primitives, and execution provenance | experiment selection, adaptive tuning policy, evidence interpretation, or catalog promotion |

There are two distinct promotion paths:

1. **Experiment promotion: Labs → Field Kit.** Review establishes that an
   experiment asks a useful question and is safe, bounded, reproducible, and
   honest about cost. It does not establish that the tested model,
   configuration, or tool is good or recommended.
2. **Product promotion: Labs → Temper and Results.** Review of accumulated
   evidence may accept a qualification profile for Temper and a sanitized
   explanatory record for Results. A Field Kit run never performs this
   promotion itself.

Temper consumes neither moving Labs state nor Field Kit's current experiment
catalog. Field Kit depends on Temper's stable public surfaces; the dependency
never points in the other direction.

## One home per experiment

The editable experiment has one authoritative home in Labs. Promotion creates
an immutable, content-identified snapshot in Field Kit. That snapshot is
history—exactly what a user ran—not a second editable source. Any change that
can alter applicability, consent, execution, measurement, or interpretation
creates a new experiment version, including changes to:

- the experiment prompt or research question;
- machine buckets or applicability predicates;
- inputs, candidates, parameter bounds, or stop rules;
- cost estimates or renewed-consent thresholds;
- measurement procedure or evidence schema; and
- cleanup, privacy, or invalidation policy.

The Field Kit index may point to a newer promoted version, pause one, or stop
offering a retired experiment. Existing run identities continue to reference
the immutable version they used.

## Promoted experiment requirements

Labs' promotion rules must reject a package unless it declares and passes
review for at least:

- a precise question and the decision the evidence can inform;
- immutable experiment identity, version, origin, and content hashes;
- minimum compatible Temper protocol/binary requirements and exact external
  inputs;
- machine-readable hard applicability predicates and separately labeled
  advisory relevance signals;
- versioned bucket definitions rather than bucket names whose meaning can
  drift;
- estimated fixed runtime plus separately labeled variable setup/download
  time, network bytes, temporary and retained disk, memory pressure, service
  disruption, and any paid-provider exposure;
- the user choices and data boundaries each consent authorizes;
- adaptive parameter bounds, maximum attempts and total cost, stop conditions,
  and thresholds that require renewed consent;
- the evidence/result shape, required conditions, deviation log, and
  provenance inputs;
- interruption, resume, keep-or-restore, and cleanup behavior;
- local-only output by default, with no telemetry or automatic submission;
- hermetic validation of metadata, prompt/package identity, selection logic,
  refusal paths, and cleanup planning; and
- invalidation, pause, and retirement triggers.

Promotion qualifies the experiment procedure, not its hypothesis. An
experiment intended to gather the first witness for a new machine bucket may
therefore be promoted without a positive result for that bucket, provided its
mechanics and safety envelope meet the promotion bar and the missing evidence
is explicit.

## Root prompt and consent flow

The Field Kit root prompt is discovery and orchestration, not an ambient
installer:

1. Verify the Field Kit snapshot and the compatible Temper executable.
2. Ask Temper for canonical machine and local-cache/software facts without
   mutation.
3. Apply hard experiment predicates deterministically. The agent may explain
   advisory relevance and rank what seems useful, but it may not invent
   eligibility or silently weaken a refusal.
4. Present each applicable experiment's purpose, applicability reason,
   estimated time and resources, data boundary, effects, cleanup, and
   uncertainty in the estimate.
5. Obtain explicit opt-in for named experiment versions. Discovery alone
   performs no download, installation, service change, or submission.
6. Hand one opted-in package and the frozen machine facts to its experiment
   prompt. Run experiments independently unless a promoted package explicitly
   declares an exact prerequisite identity.
7. Write the complete local session report and offer the user a reviewable
   share/export action. Never upload it automatically.

The agent is allowed to interpret behavior and choose a next attempt only
inside the promoted envelope. Every attempt records the inputs, observations,
choice, and rationale. Crossing a model/tool/data-boundary choice, resource
ceiling, attempt limit, or declared consent scope stops for a new human choice;
recommendation is never consent.

## Temper execution and provenance

Temper exposes narrow facts and effects that are useful outside any one
experiment: canonical machine reporting, exact lock resolution or validation,
software install/check/remove, model artifact verification, isolated config
rendering, scoped foreground service lifecycle, measurements, and
provenance-guided cleanup. Exact command names and RESULT lines remain C11
work; Temper does not embed the Field Kit root prompt or an open-ended AI
experiment loop.

The executable `temper-field-kit-binding/v1` is the Temper-material layer of a
run identity. It binds machine facts, exact Temper bytes, ordered software
lock/receipt identities, manifest lock, and rendered generation. The promoted
Field Kit package/session adds its independently owned experiment identity,
metadata and prompt hashes, consent record, attempts, deviations, observations,
and report identity. Temper need not parse that moving experiment envelope.

This split keeps provenance at its trust boundary: Temper states what it
executed and observed about its managed material; Field Kit states which
promoted experiment and adaptive decisions used those facts; Labs states what
conclusion, if any, the evidence supports.

## Replacement gate for the original Field Kit

The original implementation may be removed or archived only after the new
Field Kit demonstrates, over Temper's public surface:

- read-only machine discovery and deterministic experiment applicability;
- per-experiment cost explanation and explicit opt-in;
- one fixed mechanical experiment and one bounded adaptive experiment;
- exact package, prompt, Temper-material, attempt, and report identities;
- deviation/conclusion capture and a local exportable evidence packet;
- interruption handling plus keep-or-restore behavior; and
- hermetic selection/refusal/cleanup coverage followed by an explicitly
  authorized scratch round-trip.

Parity protects the useful consent and evidence behavior. It does not require
preserving the old repository's Bash layout, clone-the-stack installation
model, command names, or serialization accidents.
