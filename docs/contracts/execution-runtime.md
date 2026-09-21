# Direct execution locks and supervised probes

This development surface consumes an exact execution lock. It does not resolve
updates or select software. The existing manifest commands and `execution export`
remain available for issued clients.

```
temper execution inspect --lock FILE
temper execution prepare --lock FILE --root PATH --installation ID
temper execution render --lock FILE --root PATH --installation ID
temper execution serve --lock FILE --root PATH --installation ID --generation SHA256 --listen 127.0.0.1:PORT --status-file FILE
temper execution remove --lock FILE --root PATH --installation ID
```

`inspect` is read-only. Its JSON includes the lock checksum, execution digest,
profile, layouts and request defaults. `prepare` installs exact software, fetches
selected model files, renders, checks installed bytes and returns the generation
and material binding as JSON. Each underlying effect remains independently
recoverable and repeatable; a failed preparation can leave a partial installation.
`render` verifies and binds already installed material without installing or
downloading. Both commands derive private temporary legacy inputs inside Temper;
clients never coordinate or retain the four compatibility exports. `remove`
uses the exact software receipt and retains system-managed packages.

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

Temper discovers and validates router/engine membership and listener ownership.
SIGTERM to the foreground Temper child requests bounded group shutdown; Temper
rechecks identities before TERM and KILL. Only a final stopped snapshot proving
the group absent and listener closed permits cleanup. Unknown identity, failed
observation, forced termination of Temper, or missing final status cannot imply
successful shutdown. Field Kit measures the supplied identities and chooses its
own limits; these primitives contain no experiment protocol or threshold policy.
