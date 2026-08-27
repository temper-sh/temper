# `temper field-kit` contract

Status: active pre-release baseline surface, 2026-08-27.

`temper field-kit` executes release-reviewed promotion content embedded in the
Temper binary. It never reads Labs or an adjacent Field Kit checkout by
default. `--catalog PATH` and `--facts PATH` are explicit development/review
overrides and receive strict canonical validation.

## Read-only commands

```text
temper field-kit baseline verify [--catalog PATH]
temper field-kit baseline inspect [--facts PATH] [--catalog PATH]
temper field-kit baseline explain ID@REV [--facts PATH] [--catalog PATH]
```

- `verify` validates catalog/package/reference hashes and refuses any active
  package whose exact runtime protocol identity is not supported by this
  Temper release.
- `inspect` detects canonical local machine facts unless overridden, lists
  inactive packages, and evaluates active hard applicability plus advisory
  relevance without mutation.
- `explain` requires one active applicable exact revision and writes the exact
  consent disclosure. It performs no download, installation, service start,
  cleanup, session write, or upload.

Success returns exit 0. Invalid syntax returns 2. Validation, applicability,
or read failure returns 1.

## Guided user workflow

```text
temper field-kit baseline run ID@REV --root NEW_PATH \
  [--outcome keep|restore] [--session PATH] [--report PATH] \
  [--facts PATH] [--temper PATH] [--catalog PATH]
```

This is the normal user-facing entry point. For a new root, Temper validates
the active applicable package, prints the exact disclosure, prompts for a
keep-or-restore outcome when omitted, and requires exact `yes` consent before
creating anything. It then starts the session, advances the declared stages,
and finishes the report. Restore asks for a second exact `yes` immediately
before marker-guarded root removal.

The session and report default to `NEW_PATH.session.json` and
`NEW_PATH.report.md`. Rerunning the same command discovers that session,
verifies its baseline, root, and optional outcome, and resumes from the first
pending stage. The low-level commands below remain the automation, audit, and
recovery surface.

## Low-level consent and session start

```text
temper field-kit baseline start \
  --baseline ID@REV \
  --root NEW_PATH \
  --disclosure PATH \
  --outcome keep|restore \
  --consent yes \
  [--session PATH] [--id ID] [--facts PATH] [--temper PATH] \
  [--at UTC_RFC3339] [--catalog PATH]
```

The command refuses before creating a root or session unless consent is
exactly `yes`. It then requires an active applicable revision, disclosure bytes
identical to the current exact explanation, a clean new dedicated root, and a
new session path outside that root. By default, the session is
`NEW_PATH.session.json` and its stable ID combines the baseline ID with the
consent timestamp. The executing binary, detected facts, and current UTC time
are also defaults; override flags exist for controlled placement,
deterministic review, and tests.

Start atomically binds a canonical session to the catalog/package/material,
machine facts, executing Temper bytes, compiled software lock, disclosure,
outcome, consent time, and root ownership marker. It materializes the exact
package and machine facts below the new root. A session commit failure removes
only that newly created root.

## Resumable execution

```text
temper field-kit baseline status --session PATH [--baseline ID@REV] [--catalog PATH]
temper field-kit baseline run --session PATH [--baseline ID@REV] [--catalog PATH]
temper field-kit baseline run-next --session PATH [--baseline ID@REV] [--catalog PATH]
```

The session carries its exact baseline ID and revision, so these commands infer
the selector. Optional `--baseline` is a consistency assertion and is refused
if it differs. A newer embedded catalog may retire a revision without
stranding an existing session: execution still requires the retained package
with the exact session-bound package hash.

`status` is read-only. `run` is the normal path for a package that explicitly
declares `temper-multi-stage/v1`; it advances all remaining stages in order.
`run-next` advances exactly one stage and remains the inspection/recovery path.
Both verify every consented immutable input before each stage, invoke only the
exact Temper operation or supported built-in protocol, validate stable output,
write local evidence, and conditionally commit that one session transition
before another stage can begin. Failure or interruption writes diagnostic
output below the dedicated root and does not advance the failed stage. Rerun
retries that convergent first pending stage. Packages predating the multi-stage
declaration are refused by `run` and remain operable through `run-next`.

The active baseline stages are install, fetch, apply, software check, artifact
check, material binding, live protocol, and outcome. The live protocol owns a
loopback foreground process group and stops on timeout, process failure,
thermal/performance warning, or at least 512 MiB swap growth. Its report retains
structured outcomes and hashes, not generated response content.

## Finish and restore

```text
temper field-kit baseline finish \
  --session PATH [--baseline ID@REV] [--report PATH] \
  [--confirm-restore yes] [--catalog PATH]
```

Finish requires all stages to have succeeded and writes the final report
outside the dedicated root. Its default is `NEW_PATH.report.md` beside that
root. A `restore` outcome additionally requires exact `--confirm-restore yes`
and a valid session-bound ownership marker before removing the dedicated root.
The external session and report remain. A `keep` outcome retains the exact
root.

No command uploads evidence. Export and Labs review are separate explicit
actions.
