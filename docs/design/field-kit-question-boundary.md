# Field Kit question packages and Temper host boundary

Status: active.

Field Kit is an independently versioned executable layer in Temper's supply
chain. Labs owns editable investigation design, Field Kit promotes and executes
immutable question packages, and Temper supplies stable machine/install/check
primitives.

```text
Labs                         Field Kit release                    Temper
editable investigation ---> question package + Python runtime -> machine primitives
promotion review             consent/session/protocol            install/check/bind/probe
```

A Field Kit protocol change does not rebuild Temper. A Temper release changes
only when the reusable host primitives themselves change.

## Ownership

| Owner | Owns | Must not own |
|---|---|---|
| Labs | editable investigation definitions, source evidence, promotion review, returned-evidence interpretation, product-promotion decisions | a runtime dependency, user consent, or silent product changes |
| Field Kit | immutable question packages/catalogs, applicability, disclosure, consent, resumable sessions, protocol code, safety policy, evidence, reports, export, and marker-guarded cleanup | editing Labs research, hidden machine effects, a production service, or automatic product promotion |
| Temper | canonical machine facts, exact software/artifact installation and verification, rendering, material binding, isolated loopback process admission, receipts, and scoped removal | question/protocol orchestration, evidence interpretation, automatic submission, or a moving Field Kit feed |

Temper contains no Field Kit catalog, question package, session, or protocol
runtime. Those independently released bytes are not copied into Temper.

## Two independent promotion gates

1. **Question-package promotion: Labs → Field Kit.** Review freezes a useful
   question and bounded procedure, exact inputs, applicability, cost, consent,
   protocol, evidence, limits, and cleanup. Promotion does not answer the
   question or recommend the subject.
2. **Product promotion: Labs → Temper/Results.** Later evidence review may
   accept a qualified product profile and public explanation. A Field Kit run
   never performs that transition.

## Immutable package and runtime identity

Labs is the editable investigation home. Promotion creates an immutable,
content-identified Field Kit question-package revision. Any change that can
affect execution or interpretation creates a new package revision, including
changes to:

- prompts, questions, inputs, candidates, parameter bounds, or stop rules;
- applicability, costs, destinations, writes, privacy, or renewed consent;
- stage order, Python protocol bytes, measurements, or evidence schemas; and
- interruption, resume, keep/restore, cleanup, invalidation, or retirement.

The Field Kit session binds the package, runner, Python interpreter, Temper
binary, canonical machine facts, exact software and manifest locks, receipts,
rendered generation, outcome, stage evidence, and report. The pure
`temper-field-kit-binding/v1` remains the inner Temper-material identity.

## Runtime sequence

The independently released Field Kit command:

1. verifies its own catalog/package/referenced hashes;
2. asks `temper machine facts` for canonical non-identifying facts and
   evaluates hard applicability without mutation;
3. prints exact purpose, evidence, cost, data, effect, and cleanup disclosure;
4. requires explicit question selection, keep/restore choice, and exact consent;
5. creates a new dedicated root plus external session/evidence paths;
6. calls Temper's install/fetch/apply/check/bind primitives in the declared
   order and commits each successful stage once;
7. runs the exact package-owned Python protocol over `temper probe serve`;
8. writes external local evidence and a report; and
9. for restore, asks again, calls receipted software removal, validates the
   exact ownership marker, and removes only the dedicated root.

Failure retains diagnostic stage output without advancing the session. Rerun
reconciles and retries the first pending stage. Nothing uploads automatically.

## Distribution

Temper and Field Kit have separate signed release identities. Temper's identity
covers the binary that can change the machine. Field Kit's identity covers the
catalog, packages, Python runtime code, and protocol bytes that decide how those
primitives are composed.

The initial checkout runtime uses only the Python standard library. Product
distribution should install a pinned Python interpreter and immutable Field Kit
bundle through Temper's ordinary software-supply system, then expose a
`field-kit` launcher. Updating that bundle follows the Field Kit release
channel and does not update Temper.

Release checks for Field Kit must verify package/catalog hashes, run hermetic
session/interruption/tamper/cleanup tests, and exercise compatibility against
the minimum supported Temper binary. Heavy live verification remains explicit
machine-owner work.
