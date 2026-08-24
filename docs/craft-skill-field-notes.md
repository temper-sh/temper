# Craft skills — real-work field notes

Temper is the craft set's first sustained real-product test. These notes close
the secondary objective in `PLAN.md` §1: after each named phase through
M5/1.0, record how the skills affected actual work and propose a narrow
improvement only when the phase supplies evidence for it.

This is not a model-eval log, product evidence, or permission to change the
skills repository. It creates no synthetic Temper work. A note may — and often
should — conclude that no skill change is warranted.

## Closeout format

Each note records:

1. phase, date, and the product artifacts or decisions reviewed;
2. skills that materially influenced the work — omit incidental loads;
3. guidance that helped and where it changed the result;
4. missing, misleading, over-specific, or costly guidance, including a defect
   or near miss when one exists;
5. proposed improvement and owning skill/seam, or **no change warranted**;
6. disposition: proposed to the skills plan, accepted, declined, or awaiting
   more real-world evidence.

Keep product-specific facts here. Any proposal transferred to the skills plan
must state the context-independent invariant and preserve the skills
repository's routing, stance, and eval gates.

## Baseline — M1 complete plus M2 work through 2026-08-21

**Status:** retrospective M1 closeout and provisional M2 observation. This
does not close M2 Phase A or B; each still receives its own note.

**Artifacts reviewed:** native manifest/lock/render/check contracts and tests;
software-supply catalog, software lock, adapter family, signed-catalog
lifecycle, tested-status read, installation contract, and pure install planner.

### What helped

- **Data modeling:** surface-first and one-home-per-fact kept user intent,
  resolution, tested evidence, observed installation, and removal authority in
  separate manifest, lock, catalog, receipt, and root-state artifacts. That
  separation prevented the lock from claiming an installation and the receipt
  from becoming update policy.
- **Code organization and unit design:** the keyed adapter family gave
  `system-package` a portable strategy while keeping Homebrew and uv vendor
  details at concrete edges. Resolver reads, pure selection, inspection, and
  installation effects remained separate instead of growing OS/package-manager
  conditionals in CLI verbs.
- **Reliable effects:** staged validation, one commit point, explicit unknown
  outcomes, and reconciliation shaped both catalog activation and software
  installation. They prevented a package-manager effect from being mistaken
  for an atomic local transaction.
- **Testing:** test-by-unit-kind produced pure policy/planner tables, focused
  adapter contract suites, and hermetic effect orchestration. The result tests
  product outcomes without invoking the network, Homebrew, uv, or the live
  service.

### What the work exposed

1. **Provenance needed a smaller trust grain.** A software lock may combine
   independently authorized catalog, experiment, and base identities. A
   top-level source list says who participated but cannot justify each
   independently trusted decision.
2. **Shared-resource removal needed stronger authority.** Per-installation
   receipts are history, not permission to remove a shared package. Safe
   uninstall needs root-wide acquisition provenance, prepared/active claims,
   exact identity, serialized final retirement, and preservation when proof is
   absent.
3. **Effect testing needed named crash boundaries.** Happy-path plus generic
   failure tests do not cover the durable worlds between prepared intent,
   provider effect, observed poststate, receipt publication, and claim
   finalization.
4. **Long-lived contract evolution is a distinct question.** Catalogs may
   change more often than the binary while locks and receipts must remain
   interpretable. Schema labels alone do not answer reader/writer coexistence,
   canonical identity, absent/null semantics, or removal of old forms.
5. **Unit-level lifecycle remains under-owned.** The reliable shared-resource
   protocol now has a home, but a unit or library surface still needs a clear
   owner-versus-borrower rule for starting, stopping, closing, cancellation,
   retries, logging, and global configuration.

### Proposals and disposition

- **Accepted into existing craft guidance:** provenance at the independently
  trusted grain (`data-modeling`); destructive/shared-resource authority
  (`reliable-effects`); failure-boundary restart tests (`testing`).
- **Planned:** deterministic craft-set verifier; `contract-evolution`; the
  `unit-design` half of resource/lifecycle ownership after another phase note
  confirms the seam.
- **No change warranted:** no Temper evidence currently supports changing
  routing descriptions, adding a craft meta-skill, or expanding functional or
  language coverage. Those remain governed by their existing set-level
  evidence gates.

No model eval was needed to reach these observations. They came from product
contracts, implementation seams, and concrete failure windows encountered in
the phase work.
