# Temper

Temper installs, tunes, and verifies a local-LLM stack on Apple Silicon — and
refuses to pretend. Every recommendation traces to a Labs decision packet, a
measurement on real hardware, and a Results record people can audit; every
number carries the conditions it ran under, and anything unmeasured is
labeled unmeasured.

This is `temper-sh/temper`, the release repository: it ships reviewed
configuration and the minimum probe environment. Setup, wizard, generator,
lock, reviewed catalog profiles, acceptance suites, machine report, harness
adapters, and the probe *base* live (or will live) here — the probes
themselves belong to field-kit, which runs its stages over the base Temper
installs.

## Status

**Product scaffold — no shipping code yet** (2026-08-14). The spec is settled
enough to build against; the execution plan below is the working order. The
legacy `local-ai-setup` repo remains the installer of record for the one
machine currently running the stack, and stays so until this repo reaches
parity (plan, milestone M5). No release is implied by this scaffold.

## Start here

- [Product spec](docs/SPEC.md) — what Temper is, for whom, and its
  non-negotiable principles.
- [Execution plan](docs/PLAN.md) — milestones M0–M5 with acceptance gates,
  interface contracts, the design discipline, and the owner decision
  register.
- [Working rules](AGENTS.md) — boundaries and the product quality bar.

## Boundaries (the org)

| Repo | Role |
|---|---|
| `temper-sh/labs` | decides and gathers evidence; owns experiments, dossiers, and product handoffs |
| `temper-sh/results` | explains reviewed evidence to people |
| `temper-sh/field-kit` | executes frozen portable tests on consenting machines |
| **`temper-sh/temper`** | **ships reviewed configuration and the minimum probe environment** |

Temper consumes reviewed packets and accepted product handoffs, never a
moving Labs directory. Reports and witnesses flow back through Labs review;
nothing here phones home, runs a daemon, or invokes sudo.

## License

[0BSD](LICENSE). The tree redistributes no third-party file.
