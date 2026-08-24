# Labs product promotion — C8

Status: **Temper side provisionally approved by owner for refinement**,
2026-08-25. This approval does not authorize an adjacent-repository change:
Labs must still adopt the writer side in its own repository before the C8
compiler is implemented or a real packet is accepted. This document changes no
adjacent repository and promotes no real profile.

## Decision

C8 is `temper-labs-product-promotion/v1`: one immutable Labs review packet
proposes one exact revision of one C7 qualification profile. Temper's catalog
compiler consumes only an explicitly supplied packet and an explicitly
supplied set of exact C7 dependencies. Compilation is pure, canonical, and
one-to-one.

The boundary exists to preserve three separate authorities:

1. Labs records what evidence exists, what it supports, what it does not
   support, and the exact candidate product facts it reviewed.
2. Temper release review decides whether to invoke the compiler and include
   the emitted profile in a qualification-catalog snapshot.
3. The user alone selects layouts, tools, harness integrations, modes, and a
   preferred member in `manifest.yaml`.

A packet can propose `WATCH`, `LAB`, `QUALIFIED`, `REJECTED`, or `RETIRED`.
It cannot grant consent, install anything, add a recommendation-set member,
publish Results, or mutate a catalog. Those remain distinct actions with
distinct writers.

## Not C12, not a session, not a generic component handoff

Two Labs outputs use the word promotion and must not share a schema:

| Boundary | Certifies | Destination | Temper reads it? |
|---|---|---|---|
| C12 experiment promotion | a bounded experiment is useful and safe to offer | Field Kit immutable experiment catalog | no |
| C8 product promotion | reviewed evidence supports one exact product/profile disposition | Temper C7 compiler | yes, only when explicitly supplied |

A Field Kit session may be one evidence source inside Labs review. Its
experiment identity, prompt, consent, attempts, adaptive decisions,
observations, deviations, and report remain in the session packet. C8 cites
the reviewed packet identity and extracts only the product facts and
public-safe claims accepted by Labs.

The historical `field-kit-runtime-profile/v1` packet is likewise evidence
metadata, not C8. `stack-manifest` or `external-lab` describes how the original
Bash oracle could inspect or execute that exploratory packet. It does not
become a C7 engine, install method, support claim, or generic execution path.
Labs may cite an exact legacy packet and hash while reviewing a C8 candidate;
Temper never compiles that packet directly.

Labs also has a generic product-handoff lifecycle for reusable code,
protocols, and repositories. C8 does not replace it. A new maintained engine
adapter may need a generic product handoff, while a tested engine/runtime
combination needs a C8 profile packet. Neither output implies the other.

## One packet, one profile revision

The unit of review is the smallest profile revision a consumer may accept,
reject, supersede, or invalidate independently. A packet therefore has exactly
one `target` and exactly one candidate body. An experiment that supports an
artifact profile, an engine profile, and two runtime profiles produces four
packets, possibly sharing the same immutable evidence sources.

This cardinality buys:

- claim-level provenance without a packet-wide trust shortcut;
- a deterministic one-input/one-output compiler;
- independent status and retirement history for each product fact;
- no partial success when one output of a multi-row packet is invalid; and
- review diffs whose scope matches the catalog document being changed.

Shared evidence is referenced, not copied. The C8 packets are immutable
historical snapshots; the emitted C7 document is the release-owned public
projection. That deliberate snapshot relationship is not two mutable homes.

## Packet identity and canonical bytes

The packet has stable lowercase `id`, positive integer `revision`, and,
outside its own bytes, the SHA-256 of canonical YAML. A corrected decision or
candidate creates a new packet revision. `supersedes`, when present, is an
exact packet reference with the same ID; it does not imply the target profile
has the same revision history.

Packets use the same strict canonical-YAML rules as C7: one document, known
fields only, no aliases or duplicate keys, canonical scalars, deterministic
map/set order, final newline, and exact byte round-trip. The compiler hashes
the supplied canonical packet bytes; it never trusts a caller-supplied
self-digest.

```yaml
schema: temper-labs-product-promotion/v1
id: <stable promotion id>
revision: <positive integer>
supersedes:                         # absent on first packet revision
  schema: temper-labs-product-promotion/v1
  id: <same promotion id>
  revision: <previous packet revision>
  sha256: <previous canonical bytes>
```

Packet IDs identify a review lineage, not a model, experiment, Field Kit run,
or C7 profile. Those identities remain explicit separate fields.

## Complete packet shape

The notation below is schematic. Angle-bracket values are not claims or seed
data.

```yaml
schema: temper-labs-product-promotion/v1
id: <stable promotion id>
revision: <positive integer>
supersedes: <optional exact prior C8 packet reference>

target:
  schema: <one of the six C7 profile schemas>
  id: <target profile id>
  revision: <target profile revision>
  supersedes: <optional exact prior C7 profile reference>

decision:
  status: WATCH | LAB | QUALIFIED | REJECTED | RETIRED
  status_reason: <why this exact candidate has this disposition>
  decided_at: <RFC 3339 instant>
  reviewers: [<stable Labs reviewer identities>]
  accepted_claims: [<claim ids from evidence below>]
  forbidden_generalizations:
    - <a nearby claim the evidence does not support>
  confounds:
    - id: <stable confound id>
      effect: <how interpretation is limited>
      disposition: bounded | unresolved | invalidates-claim
  gates:
    - id: <schema-specific qualification gate id>
      result: pass | fail | not-run | not-applicable
      evidence: [<evidence ids>]
      explanation: <scoped result>

evidence:
  - id: <packet-local evidence id>
    claims: [<stable claim ids>]
    sources:
      - kind: labs-record | field-kit-session | field-kit-runtime-profile |
          upstream-record | results-record
        schema: <exact source schema>
        id: <stable source id>
        revision: <exact source revision>
        locator: <Labs-owned source locator>
        sha256: <exact source bytes>
        classification: public | private | restricted
    public_source:
      kind: product-promotion | results-record
      # product-promotion means the compiler injects this C8 packet identity.
      # results-record additionally requires schema/id/revision/sha256 here.
    scope:
      artifact_profile: <exact C7 reference when material>
      engine_profile: <exact C7 reference when material>
      runtime_profile:
        schema: temper-qualification-model-runtime/v1
        id: <target or dependency id>
        revision: <exact revision>
      machine_bucket: <exact C7 bucket reference when machine-dependent>
      mode: <exact semantic mode id when mode-dependent>
      co_residents: [<exact runtime references plus placement>]
      harnesses: [<harness ids plus integration revisions>]
      conditions:
        os_build: <observed build or not-applicable>
        wired_limit_mib: <observed value or not-applicable>
        wired_limit_source: <observed source or not-applicable>
        power: <observed condition or unmeasured>
        thermal: <observed condition or unmeasured>
        load: <observed competing load or unmeasured>

candidate:
  title: <short factual title>
  summary: <evidence-scoped description>
  what_this_means: <one plain-language user line>
  roles: [<stable role ids>]
  applicability: <C7 applicability block>
  dependencies: [<C7 exact relationship/profile references>]
  data_boundary: <C7 data-boundary block>
  known_failures: [<C7 failures citing packet-local evidence ids>]
  invalidation_triggers: [<C7 triggers>]
  spec: <body for target.schema, including evidence-id uses>

sanitization:
  public_candidate_reviewed: true
  excluded_classes:
    - raw-user-content
    - private-corpus-content
    - credentials
    - prompts-not-approved-for-publication
    - machine-identifying-values-outside-the-C7-bucket
  redactions:
    - source: <evidence source id>
      class: <excluded class>
      treatment: omitted | aggregated | replaced-by-public-record
  reviewer_statement: <why candidate plus public sources are safe to compile>

catalog_consideration:
  recommendation_review: separate
  comparisons: [<optional exact peer profile references for later review>]
  note: <nonbinding advice to the cross-profile release review>
```

`catalog_consideration` is review input only and never enters the emitted C7
profile. Recommendation is a relation among multiple qualified profiles; a
one-row packet cannot establish that set by itself.

## Candidate shape and exact pins

`candidate` is deliberately the C7 common envelope minus fields owned or
generated by the release boundary:

- target schema/ID/revision/supersession live in `target`;
- status and reason live in `decision`;
- public evidence inventory is generated from `evidence`;
- exact C8 provenance is generated from the packet's canonical identity; and
- the remaining common fields plus typed `spec` are copied without semantic
  translation.

All immutable subject pins live in the target schema's typed `spec`; all C7
dependencies are full schema/ID/revision/SHA references. A packet cannot say
“current,” “latest,” “the installed version,” or “whatever the experiment
used.” If Labs has not resolved the exact facts required by the target schema,
the correct disposition is `WATCH` or `LAB`, not a free-form escape field.

## Evidence, privacy, and public provenance

C8 retains exact source identities at the trust boundary where Labs can audit
them. C7 receives only the public-safe source selected by review:

- `public_source.kind: results-record` copies an exact, already-sanitized
  Results identity into C7; or
- `public_source.kind: product-promotion` cites the C8 schema/ID/revision and
  canonical packet digest injected by the compiler, without copying its raw
  source locators or contents.

The second form permits a catalog row to remain auditable even when Labs or
some raw evidence is private. Whether the packet itself is published is an
organization policy decision; its stable identity and digest are sufficient
to request an authorized audit.

Every measured candidate value and known failure cites packet-local evidence
IDs. Each ID declares the exact claims it supports. `decision.accepted_claims`
is a subset of those declared claims, and the compiler refuses any candidate
use outside that subset. A source classified `private` or `restricted` may
support an accepted claim, but its locator and contents are never copied to
C7. Sanitization does not weaken the evidence identity; it changes only what
the public projection exposes.

The packet may record negative and conflicting evidence. `accepted_claims`
states the reviewed conclusion; `confounds`, failed gates, known failures, and
forbidden generalizations prevent a positive measurement on one axis from
silently becoming a broader claim.

## Status-specific product bar

All statuses require exact target identity, a typed candidate that validates
structurally, a decision reason, invalidation/recheck triggers appropriate to
the status, and completed sanitization review.

### `WATCH`

- records a precise product question or candidate worth monitoring;
- may omit runtime witnesses, but never the immutable facts already known;
- names intake/re-check triggers; and
- cannot be a dependency of a `QUALIFIED` profile or enter a recommendation.

### `LAB`

- pins every material required to execute or inspect the candidate;
- declares required gates and records pass/fail/not-run honestly;
- carries exact applicability as a hypothesis, separately from witness scope;
- records known confounds and missing evidence; and
- cannot be presented as qualified, supported, or recommended.

### `QUALIFIED`

- every target-schema qualification gate is present and passes or is explicitly
  not applicable for a schema-defined reason;
- every claimed machine/mode/co-resident scope has a complete witness key;
- all exact dependencies are `QUALIFIED` revisions in the supplied C7 set;
- all measurements and failures have accepted claim-level provenance;
- applicability does not exceed the evidence reuse rules;
- a runtime profile has measured first-attempt task success and regression
  disposition before throughput claims; and
- no unresolved confound invalidates an accepted claim.

Qualification remains exact and scoped. It does not imply recommendation,
selection, consent, installation, residency, or preference.

### `REJECTED`

- names the failed claim or product bar precisely;
- cites the failure evidence or deterministic refusal;
- distinguishes “bad in this exact scope” from “not tested elsewhere”; and
- retains useful known failures and reconsideration triggers without making
  the candidate runnable or recommendable.

### `RETIRED`

- targets a prior non-retired profile lineage;
- names the support, safety, upstream, or product reason for retirement;
- retains the last exact product facts and historical evidence reference; and
- states whether an explicit replacement profile exists, without selecting it.

## Pure compiler contract

Conceptually:

```text
compile(canonical C8 packet bytes, exact C7 dependencies, exact buckets)
  -> canonical one-profile C7 bytes | typed refusal
```

The compiler performs no filesystem discovery, adjacent-repository read,
network request, catalog lookup by “latest,” Results publication, signature,
or mutation. The composition root must supply every byte it may read.

The deterministic field mapping is:

| C7 output | C8 source |
|---|---|
| `schema`, `id`, `revision`, `supersedes` | `target` |
| `status`, `status_reason` | `decision` |
| title, summary, meaning, roles, applicability, dependencies, data boundary, failures, invalidation, typed spec | `candidate`, copied exactly |
| evidence IDs, claims, scopes | `evidence`, after accepted-claim and scope validation |
| public evidence source | exact Results reference or injected packet reference from `evidence.public_source` |
| `promotion` | injected schema/ID/revision/SHA-256 of the canonical C8 packet |

For every evidence record the compiler canonicalizes `scope`, computes the C7
scope key, and drops C8-only `sources`. The key preimage is canonical
`temper-qualification-evidence-scope/v1` YAML containing the scope fields but
not `key`; this is the same pure computation exported by C7 validation, not a
second Labs-owned encoding. The compiler then validates the complete emitted
document under the target C7 schema and canonical-encodes it. Same inputs
always produce byte-identical output.

Compiler success means only “this packet can become this profile document.”
Including that document in a catalog snapshot is Temper release review's
separate commit. The future publication effect stages the candidate profile
and complete index, validates every reference and digest, then commits the
catalog once; the pure compiler itself has no commit point.

## Refusals

The compiler rejects at least:

- noncanonical C8 bytes, an unknown field/schema/status, wrong prior-packet
  digest, or a forked/skipped packet supersession chain;
- a target schema outside C7, invalid target profile transition, missing exact
  target dependency, wrong dependency digest, dependency cycle, or absent
  bucket;
- a candidate field that does not belong to the target typed schema, a missing
  exact pin, or an untyped extension payload;
- a candidate evidence reference not declared in `evidence`, a used claim not
  in `decision.accepted_claims`, or an accepted claim supported by no exact
  source;
- a runtime witness with incomplete artifact/engine/runtime/bucket/mode/
  co-resident scope, or a measurement whose witness does not cover it;
- `QUALIFIED` with a failed/not-run required gate, non-qualified dependency,
  unresolved invalidating confound, unsupported applicability extension, or
  incomplete runtime task-quality evidence;
- raw source locators or private contents appearing in the candidate/public
  source, or incomplete/false sanitization declarations;
- a direct `field-kit-runtime-profile/v1` or session packet presented as C8;
- a legacy `external-lab` runner value used as installation or support
  authority;
- recommendation membership, ranking, default, selected, checked, preferred,
  consent, credential, installation, or automatic-publication semantics in the
  candidate; and
- a packet targeting more than one profile or asking for partial compilation.

Failures are typed as validation refusals, never retried as transient
operations. A caller may correct the packet in Labs and submit a new immutable
revision; Temper never repairs or rewrites the source packet.

## Hermetic acceptance fixtures

The first implementation fixture set will use fake identities and no real
product claims:

1. compile one packet for every C7 profile kind and byte-round-trip each
   emitted document;
2. compile a `LAB` runtime candidate that cites an exact legacy-style
   Field Kit evidence packet without inheriting its runner or install meaning;
3. compile one `QUALIFIED` fake runtime with complete task-quality evidence,
   then reject the same packet when task success, regression, conditions, or
   dependency status is weakened;
4. keep private source locators in C8 while proving none occur in C7 output;
5. use both public-source forms and pin the exact injected packet digest;
6. correct a packet and profile through independent supersession chains,
   proving neither chain is inferred from the other;
7. reject a direct Field Kit session, multi-target packet, recommendation,
   selection, preference, or consent leak; and
8. add the output to a fake catalog index only through a separate staged
   catalog transaction, then reject an illegal row without changing the prior
   index.

The planned native-MTP `LAB` row is not a fixture input until Labs supplies an
accepted C8 packet with exact evidence and sanitization. A legacy run or
Field Kit profile found elsewhere is inspectable evidence, not an accepted
Temper product-promotion packet.

## Cross-repository adoption gate

Adoption and final approval of C8 require an explicitly authorized Labs-side
step that:

1. adopts the exact writer schema and canonicalization rules;
2. maps Labs review gates and retained evidence to the claim-level fields;
3. creates one fake hermetic packet with no private material;
4. proves private/restricted source locators do not cross the C7 projection;
5. records how packet corrections and retirement append history; and
6. hands the canonical fixture bytes to Temper without a runtime dependency on
   a Labs checkout.

Only after that co-design may Temper implement the parser/compiler and accept
real promotion packets. No part of this document authorizes editing Labs,
publishing Results, rebuilding Field Kit, or reading moving experiment state.
