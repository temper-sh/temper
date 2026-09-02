# `temper probe serve` — isolated foreground execution

Status: executable pre-release contract, revised 2026-09-02.

`probe serve` starts one llama-swap process in the foreground for a bounded
Field Kit run. It is intentionally not Temper's production service lifecycle:
it never reads or writes launchd, never infers the legacy root, never chooses a
model, and creates no daemon or persistent process record.

## Invocation

```text
temper probe serve
  --root PATH
  --installation ID
  --generation SHA256
  [--software-lock software.lock.yaml]
  [--listen 127.0.0.1:8080]
  [--dry-run]
```

All three identities are mandatory. The generation is the exact content hash
reported by `temper apply`, not the mutable `rendered/current` pointer. Only an
IPv4 loopback address and a non-privileged port are accepted.

## Admission and process boundary

Before any process starts, Temper requires:

- a canonical software lock and canonical matching installation receipt;
- the exact generation's canonical `runtime/requirements.json`;
- every package selection named there, present under a recognized isolated
  `upstream-release` or `uv` identity in the matching receipt;
- every required executable at its generation-declared relative path inside
  the corresponding receipted payload;
- the exact generation's regular `llama-swap/config.yaml` file.

The child receives a minimal `PATH` containing only directories of the exact
receipted engine executables plus macOS system binary directories. No ambient
Python environment or executable lookup is accepted. The router runs as:

```text
llama-swap --config <exact-generation-config> --listen 127.0.0.1:<port>
```

The public software command compiles both recognized isolated installers. An
uv receipt is created only from an explicit exact software lock whose managed
CPython archive and complete wheelhouse pass the uv member's installation and
post-install inspection contract; this execution boundary never manufactures
or weakens that identity.

Router output stays attached to the invoking terminal. Cancellation sends
`SIGTERM` to the foreground process group and bounds shutdown before forced
termination. Normal exit and cancellation report `stopped`; other process
failures return exit 1. `--dry-run` completes every admission check but starts
nothing.

This slice exists so Field Kit does not need to construct paths or vendor
process supervision. Production `start`, `stop`, and `status` remain a later,
separate Temper lifecycle with stronger state and recovery contracts.
