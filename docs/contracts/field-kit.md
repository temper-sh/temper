# Temper host contract for Field Kit

Status: active pre-release host surface, revised by owner 2026-08-28.

Temper is Field Kit's installer and machine checker. Field Kit is the
independently versioned user runtime. New discovery, consent, session,
protocol, evidence, reporting, export, and cleanup behavior lands in
`temper-sh/field-kit`, not in this binary.

## Stable host primitives

A Field Kit release may compose only these public Temper commands:

```text
temper machine facts
temper software install --root PATH --installation ID --lock PATH
temper software check --root PATH --installation ID --lock PATH
temper software remove --root PATH --installation ID --lock PATH
temper fetch LAYOUT --root PATH --manifest PATH --lock PATH
temper apply --root PATH --manifest PATH --lock PATH --mode NAME
temper check --root PATH --manifest PATH --lock PATH --mode NAME --verify
temper field-kit bind --root PATH --manifest-lock PATH --generation SHA256 \
  --installation ID=SOFTWARE_LOCK
temper probe serve --root PATH --installation ID --software-lock PATH \
  --generation SHA256 --listen LOOPBACK
```

Their existing contracts remain authoritative. In summary:

- `machine facts` is a read-only canonical `temper-machine-facts/v1`
  document.
- software install/check/remove accepts a complete exact lock, operates only
  below the explicit Temper root, and uses receipts and ownership claims.
- fetch/apply/check operate on exact manifests and locks below the explicit
  root; full check streams selected artifacts against their hashes.
- bind returns the pure `temper-field-kit-binding/v1` identity over Temper,
  machine facts, locks, receipts, and rendered generation.
- probe serve admits one receipt-bound generation and owns one foreground,
  loopback-only process group. It does not choose or interpret a protocol.

Field Kit invokes exact argv directly. It must not use a shell, infer the live
legacy root, call an internal package, or parse human prose when a stable
`RESULT` or schema field exists.

## Ownership of orchestration

Field Kit owns:

1. validating its independently released catalog and package bytes;
2. detecting applicability from `temper machine facts`;
3. rendering disclosure and collecting exact consent;
4. creating and conditionally committing its external resumable session;
5. ordering and invoking the stable Temper primitives;
6. running package-owned Python protocols and applying their bounded stop rules;
7. retaining sanitized evidence and rendering reports;
8. explicit local export; and
9. second-confirmation, marker-guarded keep/restore cleanup.

Temper does not validate a moving Field Kit catalog or protocol. Field Kit
binds its own package/runner identities alongside the Temper material binding.

## Release and compatibility rule

A Field Kit package, protocol, disclosure, session schema, report, or export
change creates only a Field Kit revision/release. A Temper release is warranted
only when Field Kit needs a new generally useful machine effect or when an
existing primitive contract must make an incompatible change.

Field Kit records the exact Temper binary hash and version in each session. A
Field Kit release declares and tests its minimum compatible Temper version. A
new Temper version must retain these primitive contracts or make the
incompatibility explicit before release.

No command uploads evidence. Product/profile promotion remains a separate Labs
and Temper review.
