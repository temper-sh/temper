# Direct execution locks and supervised probes

This contract remains the fixed-process measurement path. Persistent user
availability uses the separate [managed layout contract](layouts.md) and
[operation guide](init.md#activate-inspect-and-stop). Idle unload/reload is
normal there; it still ends an observed fixed-lifetime experiment here.

This development surface consumes an exact execution lock. It does not resolve
updates or select software.

```
temper execution configure --lock FILE --preset ID --context N [--max-output N] [--compute cpu|gpu] [--max-memory BYTES] --out FILE [--dry-run] [--json]
temper execution inspect --lock FILE [--json]
temper execution prepare --lock FILE --root PATH --installation ID [--json]
temper execution render --lock FILE --root PATH --installation ID [--json]
temper execution paths --lock FILE --root PATH --installation ID [--json]
temper execution serve --lock FILE --root PATH --installation ID --generation SHA256 --listen 127.0.0.1:PORT --status-file FILE
temper execution remove --lock FILE --root PATH --installation ID [--json]
```

Each command prints a short plain summary. Clients pass `--json` and read the
one JSON document it prints on stdout; the documents below are those.

`configure` derives one exact preset execution from a frozen lock. Only the
context window, optional output default, supported CPU/GPU choice and optional Splash memory cap may change;
software, artifacts, templates, sampling and speculation remain frozen. It
refuses context changes for CrispEmbed's fixed encoder limit; rerankers carry
no generation defaults. Omitting `--max-output`, or passing zero, removes the
output cap in the derived lock. A positive value is an overridable default,
independent of the context size. The
[output-default contract](execution-lock.md#settings-and-evidence) describes engine support. It
performs no machine detection, installation, model loading or upstream lookup.
The source stays unchanged, existing different output is refused, and dry-run
writes nothing. JSON `temper-execution-configuration/v1` returns `preset`, the
explicit `settings` (`context_window_tokens`, optional `max_output_tokens`,
`compute` and `max_memory_bytes`), `path`, `changed` and `dry_run`.

`catalog compile --preset ID` compiles a current v1 catalog directly. A client
needs no Selection file. The serialized lock is owned by Temper; clients pass
it unchanged to the execution commands.

`inspect` is read-only. Its JSON includes `preset`, `presets`, selected
`compute`, allowed `compute_options` and request defaults. No command prints a
lock identity; a caller that keeps evidence hashes the lock file it owns. `prepare` installs exact software, fetches
selected model files, renders, checks installed bytes and returns the generation
and material binding as JSON. Each underlying effect remains independently
recoverable and repeatable; a failed preparation can leave a partial installation.
`render` verifies and binds already installed material without installing or
downloading. Both commands keep the lock's derived inputs in memory and write
no temporary files; clients pass the execution lock directly. `remove`
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

NuExtract's selected image projector is independently pinned, fetched and
verified with the target model. Rendering binds its exact installed path;
CPU selection disables projector offloading as well as target offloading.
CrispEmbed uses its receipted native `crispembed-server`, four catalog-selected
CPU threads and `/health/ready`. The runtime binds its exact executable and
arguments as the engine role, just as for llama.cpp. It uses the fixed sequence
limit declared by the selected encoder and has no generation controls.

For Splash 1.1.0, `prepare` installs the complete prebuilt release, including its
Python interpreter and Metal kernels. It fetches the locked target and DFlash2
files, verifies the software, and invokes that interpreter only to derive GGUF
metadata and validate the sidecar's architecture. It writes the native source descriptor, preserves the derived Qwen tokenization,
applies the selected chat template, and atomically publishes an immutable assembly.
Source weights remain in the HF cache and the assembly uses symbolic
references through the verified artifact set. Derived files have a receipt
bound to the source artifact set and exact software archive. A changed assembly is refused. Apply,
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

Supervised serving currently supports one preset on macOS. It verifies that the
generation and its installed configuration bytes match the supplied execution
lock and root before starting a process. The router gives the preset's engine
the first port of the root's engine port block (see [Temper roots](init.md));
serving refuses to start while another process listens there, and names it.
Serving remains a foreground child of its caller. A new `--status-file` is
reserved before starting the router; an existing path is refused. Atomic JSON
snapshots (`temper-probe-status/v1`) bind the Temper PID, installation root,
installation, generation, listener and router process group. They report exact
role identities (PID, group and start time), verified loopback listeners, update
time, errors, and `safe_to_cleanup`. The caller must match its child PID and
invocation and reject stale or incomplete observations.

Temper discovers and validates router/engine ancestry and listener ownership.
On macOS, an owned helper without a listening socket can make `lsof` return 1
alongside valid listener rows. Silent selection misses still pass through full
listener validation; diagnostic errors, non-loopback listeners and incorrect
router ownership remain refusals.
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
`ps`, including a child first seen after exit, receives at most two fresh
observations before refusal. It never grants ownership or signaling authority
by itself; changed bound identities remain immediate refusals. Unknown identity,
failed observation, forced termination of Temper, or missing final status cannot imply
successful shutdown. Field Kit measures the supplied identities and chooses its
own limits; these primitives contain no experiment protocol or threshold policy.
