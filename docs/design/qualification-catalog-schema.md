# Qualification catalog and profiles — C7

Status: **provisionally approved by owner for Temper-only refinement and fake
fixture implementation**, 2026-08-25. This design settles D1 as separate typed
documents over one common envelope, but remains open to evidence-driven
refinement before the v1 surface freezes. It does not seed a catalog row,
claim that a current configuration is qualified, or authorize the wizard to
select anything.

Current Temper implementation boundary: `internal/qualification` strictly
parses canonical machine-bucket, model-artifact, engine, model-runtime, tool,
mode, activity, and catalog-index documents. The shared profile envelope,
dependency-root profiles, composed runtime/mode worlds, and narrowed activity
worlds are typed; the loader verifies their derived release paths, canonical
bytes, digests,
identities, exact bucket applicability, dependency presence, compatible role,
template, and speculation surfaces, active mode permission boundaries, and
activity non-widening from a supplied in-memory bundle. The index
representation types recommendation sets so they cannot become an untyped
escape hatch. Public evidence inventories and versioned canonical scope keys
are validated for all six profile kinds. Schema-specific `QUALIFIED` gates,
runtime task-quality completeness, applicability witnesses, evidence-scope
references, and dependency qualification/lifecycle closure execute over a
complete fake six-profile chain. Nonempty recommendation sets remain an
explicit refusal until their performance and applicability projection rules
exist. All current catalog fixtures are fake and hermetic.

## Decision

C7 is a content-addressed catalog index plus six immutable profile document
kinds:

| Profile kind | Schema | Owns |
|---|---|---|
| model artifact | `temper-qualification-model-artifact/v1` | exact model, tokenizer, template, quantization, sidecar, and license identity |
| engine | `temper-qualification-engine/v1` | exact tested software identity, serving capabilities, process and service contract |
| model runtime | `temper-qualification-model-runtime/v1` | one output-affecting layout over exact artifact and engine profiles, plus its performance profile |
| tool | `temper-qualification-tool/v1` | tool core, transport, schema, permissions, backend role, and harness/model deviations |
| mode | `temper-qualification-mode/v1` | one witnessed world of exact runtime/tool bindings, placement, residency, and harness integration |
| activity | `temper-qualification-activity/v1` | a strict tool subset inside one exact mode profile |

The catalog also carries immutable `temper-qualification-machine-bucket/v1`
vocabulary documents and plural recommendation sets. A machine bucket is not
a profile and recommendation is neither qualification nor lifecycle. There is no normalized
entity graph, generic `kind: ...` record, or untyped payload whose validity
depends on consumers guessing which fields apply.

This is the durable schema decision. Go structs, YAML parsing packages, wizard
views, and compiler internals remain replaceable behind it.

## Facts and their owners

The catalog joins existing contracts; it does not become another home for
their facts.

| Fact | One writer/home | C7 representation |
|---|---|---|
| user selection, selected layouts/tools/harnesses, `preferred` | wizard once, then `manifest.yaml` is the user's | absent; a later explicit projection strips catalog annotations |
| model artifact resolution installed for one manifest | `manifest.lock.yaml` | an artifact profile owns the reviewed immutable source identity; a manifest lock still owns a user's resolved pins |
| software policy and tested versions | C4 software-supply catalog | an engine profile references one exact tested C4 identity |
| desired installed software closure | C5 `software.lock.yaml` | never copied into C7 |
| observed installed software | C6 receipt/root state | never copied into C7 |
| experiment procedure, consent, attempts, observations | Labs/Field Kit C12 and session formats | never parsed or copied into C7 |
| reviewed product/profile conclusion | Labs C8 packet, accepted by Temper release review | exact promotion provenance plus the compiled C7 document |
| human evidence explanation | Results | a stable public evidence reference, never a live runtime dependency |

Some values intentionally appear as immutable historical snapshots. A runtime
witness repeats the exact artifact/engine/runtime scope it measured, and a C8
packet retains the candidate it proposed. Those copies mean “as reviewed then”;
strict validation makes them agree at compilation time. They are not alternate
mutable sources.

## Identity, canonical bytes, and references

Every profile has stable `id` plus positive integer `revision`. The pair is
the semantic identity; the SHA-256 of its canonical YAML bytes is the material
identity. IDs use lowercase components separated by `-` or `.`, as C2 does.

An exact profile reference is always complete:

```yaml
schema: temper-qualification-model-runtime/v1
id: example-coder-llamacpp-q4
revision: 3
sha256: <64 lowercase hexadecimal characters>
```

No reader resolves “latest,” follows a qualification/lifecycle name, or chooses the largest
revision. The catalog index lists the exact documents active in that snapshot.
References form this acyclic dependency order:

```text
machine bucket       model artifact       engine       tool
                              \             /
                               model runtime
                                      \
                                       mode
                                         \
                                          activity
```

Documents are strict, single-document YAML. Unknown fields, aliases, duplicate
keys, noncanonical scalars, multiple YAML documents, missing final newline,
and bytes that do not round-trip through the canonical encoder are refusals.
Maps sort by key. Sets sort by exact identity. Sequence order exists only where
the schema calls it semantic. Digests are computed over the exact canonical
bytes and are never embedded in the document they identify.

The one self-reference exception is inside `evidence[].scope`: a scope may
name the profile containing that evidence by schema, ID, and revision without
a SHA-256 because a document cannot contain its own byte digest. Every scope
reference to another document remains exact, including its SHA-256. Scope-key
validation must prove that an omitted digest denotes the containing profile;
it is not a general shorthand for “latest.”

## Immutable revision, qualification, and lifecycle history

A published document is never edited. A correction, pin change, qualification
change, lifecycle change, applicability change, changed known failure, or
changed recommendation basis creates a new revision. `supersedes`, when
present, is an exact reference to the previous head with the same schema and
ID. It may not skip or fork a lineage. Revision 1 has no `supersedes`; every
later revision names exactly the immediately preceding revision.

C7 records two independent reviewed facts. Qualification describes evidence:

- `WATCH`: a recorded candidate whose product case or evidence plan is not yet
  ready to run;
- `LAB`: exact enough to investigate, but missing or failing qualification
  gates;
- `QUALIFIED`: the exact declared scope passed its required gates; and
- `REJECTED`: reviewed evidence rules this exact candidate or claim out.

Lifecycle describes Temper's product posture:

- `EXPERIMENTAL`: available only with an explicit experimental label while
  long-term retention or support remains unsettled;
- `SUPPORTED`: an ordinary maintained catalog member;
- `DEPRECATED`: retained for existing users while new use is discouraged; and
- `RETIRED`: preserved as history but no longer offered or supported.

The axes are not aliases. A tool such as `project_search` may be
`QUALIFIED/EXPERIMENTAL`: it works in its exact scope, while its long-term
product place is intentionally unsettled. Retirement does not rewrite that
evidence fact; a later revision can be `QUALIFIED/RETIRED`.

Valid combinations are deliberately narrow. `EXPERIMENTAL` permits `WATCH`,
`LAB`, or `QUALIFIED`. `SUPPORTED` and `DEPRECATED` require `QUALIFIED`.
`RETIRED` preserves any qualification state, and `REJECTED` requires
`RETIRED`. An initial retired revision is refused except for an initial
`REJECTED/RETIRED` review outcome.

Qualification transitions may stay unchanged, move `WATCH → LAB`,
`WATCH → REJECTED`, `LAB → QUALIFIED`, `LAB → REJECTED`, or return
`QUALIFIED/REJECTED → LAB`; changed material never inherits qualification.
Lifecycle transitions may stay unchanged, move `EXPERIMENTAL → SUPPORTED`,
`SUPPORTED → DEPRECATED`, reverse `DEPRECATED → SUPPORTED`, move any active
stage to `RETIRED`, or return an active stage to `EXPERIMENTAL`. Reopening a
`RETIRED` lineage requires `LAB/EXPERIMENTAL`; it cannot jump directly back to
qualified support. Every revision carries nonempty independent reasons and a
distinct exact C8 promotion identity.

Qualification closure is evaluated from the exact documents in one catalog
bundle. Every active `QUALIFIED` profile requires every direct dependency to
be `QUALIFIED` and not `RETIRED`; because each dependency is checked by the
same rule, this proves the transitive closure. Lifecycle narrows that closure:

- an `EXPERIMENTAL` profile may depend on `EXPERIMENTAL` or `SUPPORTED`
  qualified profiles;
- a `SUPPORTED` profile may depend only on `SUPPORTED` qualified profiles;
- a `DEPRECATED` profile may depend on `SUPPORTED` or `DEPRECATED` qualified
  profiles; and
- a `RETIRED` profile remains historical and does not require an available
  dependency closure.

This rule prevents a supported product from quietly acquiring an experimental
or retiring foundation, while allowing a whole experimental composition to be
reviewed together. It is applied both by the pure C8 compiler to explicitly
supplied dependency bytes and by the catalog loader to indexed documents.

The pure transition validator receives the previous and current envelopes plus
the already-verified SHA-256 of the previous canonical bytes. It requires one
schema/ID lineage, the immediately following revision, and an exact
`supersedes` reference before applying both transition tables and their
combination rules. It performs no
filesystem discovery or “latest” lookup. The future C8 compiler supplies that
prior material explicitly; the catalog index remains a current projection and
does not infer history from revision numbers.

An old exact witness does not become false because an engine releases a new
version. D7 is therefore settled as follows:

- a replacement in the same supported product lineage creates a new profile
  revision, changes the exact engine reference, and starts at `LAB`; the old
  revision remains immutable history but is absent from the new index head;
- when release review intentionally offers old and new combinations in
  parallel, the new combination gets a new profile ID rather than forking one
  supersession chain; and
- retiring or rejecting the old exact combination is a separate reviewed
  lifecycle/qualification revision, not a side effect of resolving the new
  engine.

The catalog index is the explicit current projection. Revision number alone
has no currentness or preference semantics.

## Common profile envelope

All six profile schemas have the same envelope and a kind-specific `spec`.
This schematic document shows the common fields; angle-bracket values are not
catalog data.

```yaml
schema: <one of the six exact profile schemas>
id: <stable profile id>
revision: <positive integer>
supersedes:                         # absent on the first revision
  schema: <same schema>
  id: <same id>
  revision: <previous revision>
  sha256: <previous canonical bytes>

qualification_status: WATCH | LAB | QUALIFIED | REJECTED
qualification_reason: <why the evidence has this disposition>
lifecycle_status: EXPERIMENTAL | SUPPORTED | DEPRECATED | RETIRED
lifecycle_reason: <why Temper has this product posture>
title: <short factual title>
summary: <evidence-scoped description>
what_this_means: <one plain-language line for the wizard or check output>

roles: [<stable role ids>]
applicability:
  machine_buckets:
    - schema: temper-qualification-machine-bucket/v1
      id: <bucket id>
      revision: <bucket revision>
      sha256: <bucket canonical bytes>
  foregrounds: [local | harness | none]
  harnesses: [<supported user-managed harness ids>]
  explanation: <why the profile is useful in this scope>

dependencies:
  - relationship: <schema-specific closed value>
    profile: <exact profile reference>

data_boundary:
  inference: local | harness-owned-remote | not-applicable
  credentials: none | harness-owned
  network:
    - purpose: artifact-download | evidence-export | provider-inference | tool-request
      destination: <named owner or exact source class>
      timing: install-only | request-time | explicit-export
  reads: [<data classes>]
  writes: [<data classes>]
  telemetry: none
  evidence_export: explicit-user-action

known_failures:
  - id: <stable failure id>
    summary: <observed limitation>
    effect: <what a user sees>
    evidence: [<evidence ids below>]

invalidation_triggers:
  - id: <stable trigger id>
    condition: <precise material or evidence change>
    consequence: return-to-lab | reject | retire | re-review-applicability

evidence:
  - id: <document-local evidence id>
    source:
      kind: results-record | product-promotion
      schema: <source schema>
      id: <stable source id>
      revision: <exact source revision>
      sha256: <exact source bytes>
    claims: [<claim ids this source supports>]
    scope:
      key: <canonical scope SHA-256>
      artifact_profile: <exact reference when material>
      engine_profile: <exact reference when material>
      runtime_profile:
        schema: temper-qualification-model-runtime/v1
        id: <self or dependency id>
        revision: <exact revision>
      tool_profile: <self or exact dependency reference when material>
      mode_profile: <self or exact dependency reference when material>
      activity_profile: <self reference when material>
      machine_bucket: <exact bucket reference when machine-dependent>
      mode: <exact semantic mode id when mode-dependent>
      co_residents: [<exact runtime references plus placement>]
      harnesses: [<exact harness ids and integration revisions>]
      conditions:
        os_build: <observed build or not-applicable>
        wired_limit_mib: <observed value or not-applicable>
        wired_limit_source: <observed source or not-applicable>
        power: <observed condition or unmeasured>
        thermal: <observed condition or unmeasured>
        load: <observed competing load or unmeasured>

promotion:
  schema: temper-labs-product-promotion/v1
  id: <C8 packet id>
  revision: <C8 packet revision>
  sha256: <C8 canonical bytes>

spec: <the schema-specific body>
```

Empty lists are explicit where an empty set is meaningful. A field is absent
only when the schema says it does not apply. `unmeasured` is a value, never an
omission pretending to be zero.

### Applicability is not evidence scope

`applicability` states where release review considers the profile useful.
`evidence[].scope` states where a cited witness actually ran. A profile cannot
claim applicability outside all supporting witness scopes unless it labels the
extension as compatibility-only and the relevant schema permits deterministic
reuse. Fit, stability, cache, and performance never transfer to another
machine bucket, runtime revision, mode, or co-resident set.

For a runtime witness, `scope.key` is the SHA-256 of the canonical tuple:

```text
artifact profile ref × engine profile ref × runtime id@revision ×
machine-bucket ref × mode × ordered co-resident placements ×
ordered harness integration revisions × conditions
```

The exact preimage is canonical YAML for
`temper-qualification-evidence-scope/v1`: the displayed scope fields other
than `key`, plus that schema ID. Mapping keys sort canonically; co-residents
sort by exact runtime identity and placement; harnesses sort by ID and exact
integration revision; empty sets remain explicit. A containing profile names
itself by schema, ID, and revision without a digest. Every other profile and
machine-bucket reference includes its exact digest.

The validator recomputes the key from those typed fields. Storing it is an
honest derived index: the scope is the source, the canonicalization rule is the
update path, and a mismatch is a refusal. A runtime scope must contain exact
artifact, engine, self-runtime, applicable machine-bucket, and semantic mode
dimensions, plus explicit co-resident and harness sets. OS build, wired limit,
and wired-limit source are observed; power, thermal, and competing load are
observed or explicitly unmeasured. Static artifact compatibility evidence uses
the smaller artifact-self scope with every condition explicitly
`not-applicable`; it cannot support runtime claims.

### Evidence follows the claim

Each known failure and every measured performance value references one or
more document-local evidence IDs. The top-level list is only an inventory.
A general source list with no claim-level join is insufficient because a
reader must be able to accept, reject, or supersede one measurement without
trusting every assertion in the profile.

Only public-safe Results or product-promotion identities enter C7. Raw Labs
paths, private corpora, prompts, user data, and Field Kit session contents stay
behind C8's review boundary.

For `source.kind: product-promotion`, the exact source identity must match the
profile's top-level C8 `promotion` identity. `results-record` carries an exact
versioned public record identity. Raw `field-kit-session/v1` and
`field-kit-runtime-profile/v1` identities are refused on that public surface;
C8 may cite them privately and emit only its reviewed public projection.

## Machine buckets

A bucket is immutable matching vocabulary, not a device SKU and not evidence
that a profile works. Its predicate uses only fields available from the named
canonical machine-facts schema.

```yaml
schema: temper-qualification-machine-bucket/v1
id: <stable bucket id>
revision: <positive integer>
title: <RAM × chip-generation × bandwidth-class label>
facts_schema: temper-machine-facts/v1
predicate:
  target:
    os: darwin
    arch: arm64
    distribution: macos
  hardware_models: [<exact supported hardware model strings>]
  chips: [<exact supported chip strings>]
  physical_memory_bytes:
    minimum: <inclusive bytes>
    maximum: <inclusive bytes>
axis_labels:
  memory: <human label>
  chip_generation: <human label>
  memory_bandwidth: <human label>
evidence:
  - kind: results-record | release-review
    id: <stable identity>
    revision: <exact revision>
    sha256: <exact bytes>
invalidation_triggers:
  - <hardware mapping or canonical-facts change that requires a new revision>
```

The predicate, not the label, decides membership. Until machine facts expose a
direct bandwidth measurement, a reviewed hardware-model set is the hard match
and `axis_labels.memory_bandwidth` explains the bucket axis. A new mapping
creates a new bucket revision; consumers never reinterpret an old name.

For v1, `predicate.target` is exactly unversioned `darwin` / `arm64` /
`macos`. The hardware-model and chip sets are nonempty, unique, and sorted;
the inclusive memory bounds are positive and `maximum >= minimum`. Evidence
references and invalidation triggers are likewise explicit nonempty canonical
sets. These constraints keep a partial or open-ended predicate from silently
matching a machine it was not reviewed to describe.

Field Kit's C12 bucket definitions are independently owned and versioned. A
same-looking name in the experiment catalog is not a C7 reference and never
joins by string accident.

## Typed profile bodies

The following bodies are structural contracts, not seed records. A concrete
profile must fill every required field with reviewed facts.

### Model artifact

```yaml
spec:
  source:
    kind: hugging-face | upstream-release
    repository: <immutable repository identity>
    revision: <exact 40-character upstream commit>
  files:
    - path: <relative path>
      sha256: <exact bytes>
      size: <exact bytes>
      purpose: weights | tokenizer | template | projector | drafter | other
  model_family: <stable family id>
  format: gguf | mlx-safetensors | safetensors
  quantization:
    family: <actual recipe family>
    recipe_revision: <exact recipe identity>
    tensor_allocation:
      - tensor_class: default
        precision: <exact storage precision>
      - tensor_class: <named override class>
        precision: <exact storage precision>
    calibration:
      state: referenced | not-applicable
      source: <exact external material reference when referenced>
  tokenizer:
    state: file
    path: <exact selected file containing it, including embedded-in-weights>
  template:
    state: file | not-applicable
    path: <exact selected file containing it when state is file>
  sidecars: [<paths of every projector, drafter, or other sidecar file>]
  declared_download_bytes: <sum of every selected file>
  license:
    id: <reviewed license identity>
    source:
      repository: <exact repository identity>
      revision: <exact 40-character upstream commit>
      path: <canonical relative license path>
    redistribution: referenced-not-vendored
```

All selected files, including sidecars, contribute to identity and the
download bill. File and sidecar sets are unique and path-sorted. The required
`default` tensor class makes the allocation total; named rows are exact
overrides rather than an advertised average bit label. A tokenizer or template
embedded in a weights file names that containing file, so embedded metadata is
still bound to exact bytes. Compatibility may be reused only when every
referenced byte is identical.

### Engine

```yaml
spec:
  software:
    catalog:
      schema: temper-software-supply/v1
      sequence: <exact sequence>
      sha256: <exact catalog bytes>
    package: <logical C4 package id>
    method: <exact installation method>
    adapter: <exact target adapter>
    target: {os: darwin, arch: arm64}
    root_version: <exact tested version>
    closure_digest: <exact tested closure digest>
  api:
    layout_contract: temper-runtime-layout/v1
    protocol: <exact protocol revision>
    streaming: <boolean>
    tool_calls:
      state: supported | unsupported
      request_schema: <exact revision when supported>
      response_schema: <exact revision when supported>
      parser_revision: <exact revision when supported>
  capabilities: [<closed capability ids>]
  process_isolation: foreground-child | isolated-service
  service_contract:
    readiness:
      protocol: http
      path: <canonical absolute path>
      expected_status: <exact HTTP status>
    shutdown:
      mechanism: signal
      signal: SIGINT | SIGTERM
      grace_period_millis: <positive integer>
    offline_after_install: true
```

The C4 reference establishes tested software identity; C7 adds composed
serving evidence. The catalog schema, positive sequence, and catalog-byte
digest identify the exact C4 snapshot. Package, method, target adapter,
unversioned `darwin/arm64` target, root version, and closure digest then select
one exact tested row from that snapshot. Engine capabilities are a closed,
sorted subset of `chat-completions`, `drafter-speculation`, `embeddings`,
`mtp-speculation`, `rerank`, `streaming`, and `tool-calls`; the streaming and
tool-call declarations must agree with that set. Supported tool calls bind
exact request, response, and parser revisions. The readiness and shutdown
conditions are executable contracts, not prose, and v1 engines must remain
offline after installation. An engine has no C7 profile dependency: it never
copies a C5 lock or claims that a local C6 receipt exists.

### Model runtime and performance profile

The runtime body is the catalog form of a C2 layout: output-affecting identity
only. Placement, residency, preload, TTL, and `ngl` remain mode facts. That
later settlement overrides the earlier broad wording that put placement in a
runtime profile.

```yaml
spec:
  artifact_profile: <exact model-artifact reference>
  engine_profile: <exact engine reference>
  layout:
    role: coder | rerank
    window: <positive raw model window>
    max_tokens: <positive generation cap below window; coder only>
    kv: q8 | f16                         # coder only
    thinking: on | off                   # coder only
    chat_template: artifact | not-applicable
    batching:
      parallel: <positive integer>
      flash_attention: auto | off | on
      batch: <positive integer>
      ubatch: <positive integer no larger than batch>
    speculation:
      state: disabled | drafter | mtp
      method_revision: <exact revision when enabled>
      sidecar: <artifact sidecar path for drafter only>
      draft_tokens: <positive integer when enabled>
    sampling:
      state: configured | not-applicable
      temperature: <canonical nonnegative decimal string when configured>
      top_p: <canonical decimal string greater than zero and at most one>
      top_k: <explicit nonnegative integer>
      min_p: <canonical decimal string from zero through one>
      seed: <explicit integer, including zero>
      unspecified_parameters: engine-defaults
  performance:
    task_success: <performance axis>
    regressions: <performance axis>
    task_time_and_tool_use: <performance axis>
    throughput: <performance axis>
    context: <performance axis>
    memory: <performance axis>
    cache_and_replay: <performance axis>
```

Every performance axis has one of these explicit forms:

```yaml
state: unmeasured
reason: <why no claim is available>
```

```yaml
state: not-applicable
reason: <why the axis cannot apply>
```

```yaml
state: measured
observations:
  - metric: <closed metric id>
    value:
      kind: <integer | decimal | duration-millis | success-fraction>
      <matching value arm>: <typed value>
    definition: <precise denominator/unit/window>
    witness: <document-local evidence id>
```

The model-runtime envelope contains exactly two sorted dependencies named
`artifact` and `engine`; they exactly repeat the body references. The loader
resolves both by full material identity. A coder runtime requires the artifact
and engine to claim the coder role, an artifact-owned template file, and the
engine's `chat-completions` capability. A reranker requires the rerank role and
engine capability. Drafter speculation additionally names an exact artifact
sidecar and requires `drafter-speculation`; MTP requires `mtp-speculation`.
None of these checks selects or installs the referenced material.

The v1 runtime layout is deliberately the strict C2 coder/rerank surface, but
it owns its own immutable types. Its `temper-runtime-layout/v1` contract must
match the engine declaration, so engine package names never stand in for
tuning compatibility. A later projection translates a user-chosen qualified
row into C2; C7 does not import manifest structs or write the user's manifest.
Placement, residency, preload, TTL, `ngl`, and `preferred` remain mode or
user-selection facts and cannot appear here.

`task_success` records attempts and first-attempt successes before any token or
throughput metric. `regressions` records the retained known-good/known-bad task
set. `task_time_and_tool_use` reports completed-task wall time, successful and
unnecessary calls, and recovery. `throughput` keeps raw prefill/decode only as
supporting detail. `context` distinguishes raw window from the qualified task
threshold under one catalog-wide definition. `memory` distinguishes resident,
full-slot, and peak values. `cache_and_replay` names exact history and cache
conditions. Every number obtains wall, swap, tune, thermal, power, and load
conditions through its witness.

Metric IDs and value kinds are closed per axis. Observations sort by metric and
witness, carry a one-line definition, and cite a document-local evidence ID.
Decimal values are strings to keep exact canonical bytes rather than inherit a
floating-point encoder's representation. Explicit zero is valid where the
metric or sampling policy permits it; absence is not another spelling of zero.
The sampling fields are the v1 request overrides. Every other sampling knob is
explicitly inherited from the exact engine version's defaults; it is never an
implicit client choice.

The closed v1 metric vocabulary is:

| Axis | Metrics and required value kinds |
|---|---|
| `task_success` | `first-attempt-task-success`, `overall-task-success`: success fraction |
| `regressions` | `known-bad-tasks`, `new-regressions`, `retained-good-tasks`: integer |
| `task_time_and_tool_use` | `completed-task-wall-time`: duration milliseconds; `recovery-count`, `successful-tool-calls`, `unnecessary-tool-calls`: integer |
| `throughput` | `decode-tokens-per-second`, `prefill-tokens-per-second`: decimal |
| `context` | `qualified-task-context-tokens`, `raw-window-tokens`: integer |
| `memory` | `full-slot-mib`, `peak-mib`, `resident-mib`: integer |
| `cache_and_replay` | `cache-hit-fraction`: success fraction; `history-tokens`, `replayed-prompt-tokens`: integer |

An integer or duration arm is a nonnegative integer. A decimal arm is a
canonical nonnegative decimal string. A success-fraction arm contains
`successes` and positive `attempts`, with successes no greater than attempts.
Exactly one arm must match the declared kind.

A `QUALIFIED` runtime requires measured first-attempt task success and a
complete regression disposition for its claimed role. In v1, complete means
that `task_success` is measured and contains
`first-attempt-task-success`, while `regressions` is measured and contains
`known-bad-tasks`, `new-regressions`, and `retained-good-tasks`. The reviewed
C8 gates decide whether those exact values clear the product bar; C7 refuses
only absent or structurally incomplete measurements. Other axes may remain
explicitly unmeasured. A recommendation may cite only measured observations.

### Tool

```yaml
spec:
  core:
    source:
      kind: github | upstream-release
      repository: <owner/name>
      revision: <exact 40-character commit>
      sha256: <exact reviewed source material>
    interface_revision: <exact tool-core contract>
  transports:
    - harness: <supported harness id>
      integration_revision: <Temper-owned render/adapter revision>
      protocol: <exact transport revision>
      request_schema: <exact versioned schema>
      result_schema: <exact versioned schema>
      description_sha256: <exact model-visible description bytes>
      affordance_deviations:
        - id: <stable deviation id>
          summary: <observed mismatch>
          effect: <user/model-visible result>
          evidence: [<document-local evidence ids>]
  permissions:
    reads: [<allowed data classes>]
    writes: [<allowed data classes>]
    executes: [<allowed command classes>]
    network: [<allowed network purposes>]
  backend:
    required_roles: [<roles the mode must furnish>]
    optional_roles: [<roles whose absence only narrows behavior>]
  failure_semantics:
    invalid_input: refuse
    permission_denied: refuse
    backend_unavailable: propagate-error | refuse
    partial_effect: report-partial | not-applicable
```

Tool transports are nonempty, unique by harness, and sorted by harness plus
exact integration revision. Their harness set exactly equals
`applicability.harnesses`. Permission read/write sets exactly equal the common
data-boundary sets, and network permission IDs exactly equal its declared
network purposes; execution permission remains a separate explicit command
surface. Required and optional backend roles are disjoint. A tool has no C7
dependency because a mode—not the tool—binds the exact runtimes that furnish
those roles.

Affordance deviations are evidence-bearing facts, not prose exceptions. The
four failure fields have no silent-success value. Selecting a tool later is
consent to its displayed consequences. Qualification merely makes the exact
core/transport/permission combination eligible to be offered.

### Mode

```yaml
spec:
  foreground: local | harness | none
  bindings:
    - id: <document-local binding id>
      role: <stable role id>
      runtime_profile: <exact qualified runtime reference>
      placement: resident | on-demand
      ngl:
        state: engine-default | explicit
        layers: <explicit nonnegative integer, including zero>
      ttl_seconds: <exact nonnegative TTL>
      preload: <boolean; true only when resident>
  tools:
    - profile: <exact qualified tool reference>
      active: <whether this witnessed world exposed it>
  harnesses:
    - id: <user-managed harness id>
      integration_revision: <Temper-owned render/adapter revision>
      required_capabilities: [<exact capabilities>]
  role_bindings:
    <role id>: <binding id>
  wall_model:
    result: fit | does-not-fit | unmeasured | not-applicable
    predicted_resident_mib: <prediction for fit or does-not-fit>
    witness: <document-local evidence id for fit or does-not-fit>
    reason: <required only for unmeasured or not-applicable>
```

Bindings are unique by both binding ID and exact runtime. `role_bindings` keys
exactly equal the common role set and each value names a binding with that
role. A local foreground has a resident coder binding; a harness foreground
has at least one exact integration. The `none` world has no roles, bindings,
tools, harnesses, or dependencies, an empty/not-applicable data boundary, and
a not-applicable wall model. This is the schema-defined exception to the
otherwise nonempty common role set.

Mode dependencies exactly enumerate each distinct runtime and tool reference.
The loader resolves them all, proves each binding role and applicability,
requires every active tool's backend roles, and finds an exact harness
transport revision. It also recomputes the sorted union of reads, writes, and
network uses from every bound runtime and active tool; disagreement with the
mode data boundary is a refusal.

This is the exact world that was witnessed, not a default world. `tools[].active`
records whether that witnessed world exposed a tool; it is not a request to
activate it for the user. The profile contains no `preferred`, `selected`, or
install authorization. A user may explicitly choose members from one or more
applicable catalog offers; render validation may call the resulting
composition qualified only when an exact qualified mode profile covers it.

The six-kind v1 deliberately has no standalone harness profile. Harness
executables are user-managed; exact integration revisions and deviations live
where they are consumed by engine, tool, and mode profiles. If M4 produces a
reusable harness entity with independent lifecycle and evidence, that is a
reviewed C7 schema revision, not an untyped v1 escape hatch.

### Activity

```yaml
spec:
  mode_profile: <exact mode reference>
  active_tools: [<exact tool references already present in that mode>]
  purpose: change | inspect | review | verify
```

An activity profile is valid only when `active_tools` is a strict subset of
the referenced mode's active tools. Its sole dependency is that exact mode
reference. Roles must exactly match the mode, and foreground, harness, and
machine-bucket applicability may only narrow the mode's applicability. The
loader retains every mode runtime binding, includes only the activity's active
tool subset, and recomputes the sorted union of reads, writes, and network uses.
Inference and credential ownership remain exactly the mode's. Any disagreement
with the activity data boundary is a refusal. An activity therefore cannot add
a tool, runtime, harness, permission, role binding, data destination, or
credential path.

The referenced mode need not already be `QUALIFIED` while an activity is in
`WATCH` or `LAB`; once the qualification gate is implemented, a `QUALIFIED`
activity requires the exact mode and every transitive dependency to be
`QUALIFIED` too.

## Catalog index and recommendation sets

The index is the only reader entry point. It pins exact bucket/profile bytes
and publishes recommendation as an unordered set.

```yaml
schema: temper-qualification-catalog/v1
revision: <positive integer>
published_at: <RFC 3339 instant>

machine_buckets:
  - document:
      schema: temper-qualification-machine-bucket/v1
      id: <bucket id>
      revision: <exact revision>
      sha256: <canonical bytes>
    path: machine-buckets/<id>/<revision>.yaml

profiles:
  - document: <exact profile reference>
    path: profiles/<kind>/<id>/<revision>.yaml

recommendation_sets:
  - id: <stable comparison-group id>
    applicability:
      machine_buckets: [<exact bucket references>]
      foreground: local | harness
      role: <stable role id>
    explanation: <why these are all sensible tradeoffs>
    members:
      - runtime_profile: <exact QUALIFIED runtime reference>
        reason: <evidence-backed reason to consider it>
        strengths: [<measured strengths>]
        costs: [<measured costs or explicit unknowns>]
```

Member order is canonical identity order and has no ranking meaning. The
schema has no `rank`, score, winner, default, selected, checked, or preferred
field. Every member must be `QUALIFIED/SUPPORTED`, applicable to the set, and
carry the measured performance observations cited by its reason. Several members may
share the same bucket/mode/role; none, one, or all may later be selected by the
user. A recommendation set is never projected into `manifest.yaml`.

The catalog index is release-reviewed Temper data. C7 v1 does not inherit the
software catalog's independent update channel, signature files, or active
pointer. Changing qualification-catalog distribution is a separate surface
decision; readers still verify every indexed document hash.

The current pure loader accepts the index bytes and a path-to-bytes bundle; it
does not read a directory, resolve a moving revision, or fetch content. Files
absent from the index may remain in the supplied bundle as immutable history,
but they have no currentness semantics and are not loaded.

## Projection to C2 and read rules

The wizard reads only the exact catalog index selected by its Temper release:

1. verify index structure and every referenced document digest;
2. match canonical machine facts to exact bucket predicates;
3. ignore non-`QUALIFIED` and `RETIRED` profiles for furnishing;
4. show `QUALIFIED/EXPERIMENTAL` profiles only as explicit experimental
   choices, keep `QUALIFIED/DEPRECATED` profiles available only to explain or
   validate existing selections, and find `QUALIFIED/SUPPORTED` mode profiles
   whose exact dependency closure is present, qualified, supported, and
   applicable;
5. show unfurnishable modes disabled with the derived refusal reason;
6. display every applicable recommendation-set member and its measured
   tradeoffs with all controls initially unselected; and
7. after explicit choices, project only the selected kind-specific `spec`
   facts into C2, then require the user to choose residency, harness enablement,
   and at most one `preferred` member.

Projection strips qualification/lifecycle status, recommendation, evidence,
prose, known failures, and promotion metadata. It does not synthesize a
layout, tool, or mode that no qualified profile covers. Re-running the wizard
against an existing manifest is advisory only.

`check` may compare exact selected pins with the catalog and report drift or
retirement. It never changes the manifest, lock, profile qualification,
lifecycle, or active catalog.

## Validation and refusal matrix

The Phase C validator must reject at least:

- an unknown schema, field, qualification/lifecycle status, role,
  relationship, performance state, or data-boundary value;
- a noncanonical document, wrong digest, duplicate identity, missing
  dependency, dependency cycle, or reference to a document absent from the
  index;
- a supersession edge across schemas/IDs, a skipped/forked head, illegal
  qualification/lifecycle transition or combination, or same `id@revision`
  with different bytes;
- a machine bucket whose predicate uses facts absent from its named facts
  schema, or a profile whose applicability names a missing bucket;
- a witness key that does not match its canonical scope, a runtime witness
  missing any artifact/engine/runtime/bucket/mode/co-resident dimension, or a
  metric citing a witness that cannot support it;
- a `QUALIFIED` profile with incomplete required gates, or a qualified mode or
  activity depending on a non-qualified revision;
- a runtime performance axis omitted instead of marked unmeasured, a number
  with no definition/witness, or a recommendation reason citing an unmeasured
  value;
- a recommendation member that is not an exact qualified runtime reference,
  is outside applicability, or carries ranking/default/selection semantics;
- a mode binding to an unselected or unavailable dependency, an activity that
  widens its mode, or any profile field that implies tool/harness consent;
- a raw/private Labs path or Field Kit session value in a C7 public evidence
  reference; and
- `preferred`, `selected`, `checked`, install, credential, consent, attempt,
  or experiment-prompt fields anywhere the schema does not explicitly own.

## Hermetic acceptance fixtures

Provisional approval permits Temper-only implementation against fake offline
fixtures. Freezing the v1 surface and consuming real product evidence still
require a later review. The first fixture set will:

1. round-trip one document of every profile kind plus two machine buckets
   through canonical bytes and an exact index;
2. compile one fake C8 packet into a `LAB` runtime profile without reading
   Labs, Results, Field Kit, or the network;
3. carry two `QUALIFIED` coder runtime profiles in one recommendation set,
   preserve distinct speed/context and quality-first performance observations,
   and prove neither becomes selected or preferred;
4. change an engine reference, require a new runtime revision in `LAB`, and
   preserve the old exact witness bytes;
5. prove static artifact compatibility can be reused while fit, performance,
   cache, and mode witnesses cannot cross scope;
6. reject each validation-matrix case, including a mode/activity consent leak;
   and
7. project a user-chosen subset to the existing strict C2 shape, then show
   that an empty explicit choice produces no selection.

No fixture makes a claim about a real model, engine, tool, harness, or machine.
Seeding reviewed rows remains M2 Phase C item 15 and requires accepted C8
inputs.
