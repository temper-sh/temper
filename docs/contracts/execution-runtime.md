# Direct execution locks and supervised probes

This contract remains the fixed-process measurement path. Persistent user
availability uses the separate [managed layout contract](layouts.md) and
[operation guide](init.md#activate-inspect-and-stop). Idle unload/reload is
normal there; it still ends an observed fixed-lifetime experiment here.

This development surface consumes an exact execution lock. It does not resolve
updates or select software. The current manifest v2 commands remain available as low-level primitives.

```
temper execution configure --lock FILE --preset ID --context N --max-output N [--max-memory BYTES] --out FILE [--dry-run]
temper execution inspect --lock FILE
temper execution prepare --lock FILE --root PATH --installation ID
temper execution render --lock FILE --root PATH --installation ID
temper execution paths --lock FILE --root PATH --installation ID
temper execution serve --lock FILE --root PATH --installation ID --generation SHA256 --listen 127.0.0.1:PORT --status-file FILE
temper execution remove --lock FILE --root PATH --installation ID
```

`configure` derives one exact preset execution from a frozen lock. Only the
context window, output allowance and optional Splash memory cap may change;
software, artifacts, templates, sampling and speculation remain frozen. It
performs no machine detection, installation, model loading or upstream lookup.
The source stays unchanged, existing different output is refused, and dry-run
writes nothing. JSON `temper-execution-configuration/v1` returns `preset`, the
explicit `settings` (`context_window_tokens`, `max_output_tokens`, optional
`max_memory_bytes`), `execution_digest`, `path`, `changed`, `dry_run`, and
`context_execution_sha256`. The latter is the existing
catalog context identity used by `context_findings`, so a reviewed test can be
matched by the wizard without depending on lock internals.

`catalog compile --preset ID` compiles a current v3 catalog directly. A client
needs no Selection file. The serialized lock is owned by Temper; clients pass
it unchanged to the execution commands.

`inspect` is read-only. Its JSON includes the lock checksum, execution digest,
profile, layouts and request defaults. `prepare` installs exact software, fetches
selected model files, renders, checks installed bytes and returns the generation
and material binding as JSON. Each underlying effect remains independently
recoverable and repeatable; a failed preparation can leave a partial installation.
`render` verifies and binds already installed material without installing or
downloading. Both commands derive private temporary internal inputs inside Temper;
clients pass the execution lock directly. `remove`
uses the exact software receipt and retains system-managed packages.

`paths` is read-only. It verifies the selected artifact sets and software
receipts, then returns `temper-execution-paths/v1` with the same `execution`
inspection document, model paths by layout, and exact managed interpreter paths
by Python package. This lets an experiment use its locked tokenizer or evaluator
without reconstructing Temper's storage paths. It starts no model or evaluator.

Changing context, batch, cache or speculation settings reuses the installed
software when its units and installation identity are unchanged. The new
execution lock still produces and verifies its own rendered generation;
execution provenance is not a software reinstallation requirement.

For Splash 1.1.0, `prepare` installs the complete prebuilt release, including its
Python interpreter and Metal kernels. It fetches the locked target and DFlash2
files, verifies the software, and invokes that interpreter only to derive GGUF
metadata and validate the sidecar's architecture. It writes the native source descriptor, preserves the derived Qwen tokenization,
applies the selected chat template, and atomically publishes an immutable assembly.
Source weights are hard-linked; derived files have a receipt bound to the source
artifact set and exact software archive. A changed assembly is refused. Apply,
check, render and serve verify the prepared assembly. Preparation never starts
inference or the native engine.

Splash serving requires Apple M3 or newer and macOS 26.4 or later. The
`server/server.py` frontend runs from the receipted bundled Python and starts the
receipted `engine/splash` with local paths. No mutable launcher or model resolver
runs. Splash startup and request diagnostics are forwarded through the router.
The frontend's home and native weight cache are under the explicit Temper
root. Request sampling defaults are set by llama-swap only when omitted, preserving
client overrides. The first start may convert weights: setup discloses a separate
cache-space estimate, and Splash checks its exact missing-cache requirement plus
2 GiB free before conversion. This cache is retained with model artifacts when an
installation's software is removed. The 24 GiB catalog memory ceiling is a runtime
setting, not evidence that every context window fits.

`--dry-run` validates the lock and arguments without writes, downloads or process
effects. Preparation dry run describes the operation; it does not promise that
missing installation prerequisites are already satisfied. Inspection has no
effects and needs no dry-run flag.

Supervised serving currently supports one layout on macOS. It verifies that the
generation and its installed configuration bytes match the supplied execution
lock and root before starting a process. Serving remains a foreground child of
its caller. A new `--status-file` is
reserved before starting the router; an existing path is refused. Atomic JSON
snapshots (`temper-probe-status/v1`) bind the Temper PID, installation root,
installation, generation, listener and router process group. They report exact
role identities (PID, group and start time), verified loopback listeners, update
time, errors, and `safe_to_cleanup`. The caller must match its child PID and
invocation and reject stale or incomplete observations.

Temper discovers and validates router/engine ancestry and listener ownership.
Splash also exposes a `frontend` role. Both Splash processes must match exact
receipted executable paths and rendered argument vectors; the native engine
must originate from that frontend. A changed or restarted frontend ends the
supervised session, as does a changed or restarted native engine.
Rapid MLX binds the exact receipted interpreter, console script and rendered
arguments as its `engine`. Its `crash-log` helper must use that interpreter,
the reviewed 0.15.2 helper script digest and one non-standard file descriptor,
and originate from the selected engine. The helper's identity remains bound
through shutdown; changed scripts, extra helpers and replacement processes are
refused. The script is not vendored. vLLM Metal binds the same facts as `frontend` and
admits only its CPython spawn worker and resource tracker from that frontend.
The worker's fixed `VLLM::EngineCore` process title is accepted without changing
its bound interpreter, PID, start time or group. Its `engine` and
`resource-tracker` roles retain their own identities. Additional workers,
arbitrary Python children and replacements are refused. These are the selected
single-worker launch shapes; broader worker topologies remain unsupported.

The router may start the engine in its own process group. Each observed PID,
start time, kernel executable path and group stays bound; an unrelated member,
changed group or replacement process is refused. A basename in `ps` is not an
executable identity. The status's `process_group_id` names the router group;
each role carries its own actual group.

The router's reviewed macOS hardware-inspection commands are also owned:
`/usr/sbin/system_profiler` for hardware/display data (including its separate
child groups), `/usr/sbin/sysctl -n kern.hv_vmm_present`, and
`/usr/sbin/ioreg -r -c IOGPU -d 1 -f`. Temper verifies the kernel executable and
exact read-only argument vectors as well as router ancestry. Arguments are
read without retaining process environment. These short-lived helpers are not
published as measured router/engine roles; their identities remain bound until
reaped for safe shutdown. A later helper invocation may have a new PID without
allowing replacement of a measured engine. Missing arguments, another path,
unreviewed flags, an unrelated group member or a changed bound identity still
refuse supervision. A status file does not grant ownership of any helper.

SIGTERM to the foreground Temper child requests bounded shutdown; Temper
rechecks every owned group before TERM and KILL, stopping child groups first.
Only a final stopped snapshot proving all owned groups absent and the listener
closed permits cleanup. Exiting children retain
their observed identity until reaped. A temporarily unavailable command from
`ps` is re-read and never grants signaling authority by itself. Unknown identity,
failed observation, forced termination of Temper, or missing final status cannot imply
successful shutdown. Field Kit measures the supplied identities and chooses its
own limits; these primitives contain no experiment protocol or threshold policy.
