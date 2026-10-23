# Bounded Kapitsa integration smoke

Status: completed on 2026-10-23 after the owner authorized native execution and
granted removable-volume access. Buffered/streamed chat and tool calls, owned
foreground shutdown, managed stop/restart and repeated start/stop passed. All
private jobs and listeners are stopped; source weights and live setup are intact.

The native run found two managed lifecycle faults: outgoing engine keepalives
were mistaken for accepted clients, and observation could catch the exact owned
launcher before exec. Shutdown now distinguishes socket direction by the local
listener; observation waits briefly for the exact frozen launch transition without
granting the launcher engine signaling authority. Whole-boundary disappearance
retries also allow a short interval for exiting inspection helpers.

The full smoke passed again after the Go dependency refresh, with follow-up
managed verification after the final ownership guard. Attempts and first failures
remain under `.scratch/0.4.0-kapitsa/`; `native-budget.json` owns the cumulative
600-second / 512-token allowance. No calibration JSON was emitted by this short
workload. Native verification establishes the same owned home across restart and
read-only empty-state inspection; populated calibration preservation is covered
by offline tests, not claimed from this run.

## Inputs and limits

Use the authored `qwen3.8-27b-mlx4-kapitsa` preset: Kapitsa v0.2.9
(`e86dea79b2751d04a3fa78bfa7746f4ebf981515`), MLX4 target
`6067b15cf581666a4aecf6af3afaba4bb5efc20c`, DFlash2 draft
`015e795645c74b1a0eeef3b570031fb62e769bc5`, ANE on, thinking off,
98,304-token context, one slot, 1,024-token prefill and a 24 GiB engine ceiling.
This checks integration with small requests, not long-context capacity or quality.

Prepared local inputs under `.scratch/0.4.0-kapitsa/`:

- `temper`: built from this working tree; rebuild after any code change.
- `selected.lock.json`: exact selected execution, compiled from the authored catalog.
- `smoke-configuration.json`: one Default layout, the selected preset preloaded,
  no harness integrations, five-minute idle expiry.

Use fresh sibling roots `execution-root` and `managed-root`, with loopback ports
18089 and 18090 respectively; run them sequentially. The root-derived engine
ports must also be free. Do not change the default root, existing jobs or sysctls.
Bound total serving to **10 minutes**, total generated output to **512 tokens**,
initial load/readiness to **180 seconds**, and warm HTTP calls to **60 seconds**.
Retain failed status and calibration; stop further requests on an ownership,
readiness or time-bound failure. Recovery uses the owning Temper stop path.

Source target files are in
`/Volumes/Crucial X10/temper-research/kapitsa-qwen27-mlx4-6067b15cf581/`.
The exact draft is already in the user's HF cache. Stage only the declared files
into a private APFS HF cache, checking their declared sizes and hashes. This may
copy **19,930,317,355 bytes** locally; retain at least 25 GiB free before staging.
No model download is authorized by this smoke. Source files and research homes
remain unchanged. The existing pinned Kapitsa archive has passed Temper's archive
inspector: 3,501,620 compressed bytes, 9,571,933 expanded bytes, 72 entries,
SHA-256 `370b2ec86b0d27fcb6cf9e19a563e30174e89146889bdf3adc1c3c5d01898d19`.
Allow at most 128 MiB of exact software downloads if installer cache reuse is
unavailable. Never copy the research calibration into either test root.

## Foreground execution

From the Temper repository, with `HF_HUB_CACHE` set to the private staged cache:

```sh
.scratch/0.4.0-kapitsa/temper execution prepare \
  --lock .scratch/0.4.0-kapitsa/selected.lock.json \
  --root .scratch/0.4.0-kapitsa/execution-root \
  --installation kapitsa-smoke --json
```

Repeat preparation and require no download or generation change. Use its returned
generation in `execution serve`, passing the same lock/root/installation,
`--listen 127.0.0.1:18089`, and a new absolute `--status-file` path. Verify
`/v1/models`, exact native executable/argv ownership and readiness before calls.

Send four bounded chat-completion requests to the selected preset ID:

1. Buffered, `max_tokens: 16`: `Reply with the single word OK.`
2. Streamed, `max_tokens: 16`: the same request; require a valid terminating
   stream with no duplicated content.
3. Buffered, `max_tokens: 128`: `Call lookup_weather for Stockholm.` Supply one
   function `lookup_weather` with a required string `city` and no extra fields.
   Require a named tool call with valid JSON arguments. Repeat streamed with
   at most 128 tokens and assemble the tool-call deltas before validation.

Interrupt only the foreground Temper process that this run started. Require a
successful stopped status, disappearance of its recorded engine lifetimes, and
free router/engine listeners. Use `execution verify-stopped --status-file FILE`
when shutdown reports unresolved ownership; a failure never grants permission
to kill an untracked process. Read `execution paths --json` to retain the local
calibration hashes and verify that inspection changes no files.

## Managed layout and remembered selection

The saved configuration and both private roots are already prepared. Activate
explicitly on the private listener. `configure --prepare` also starts Default on
its remembered/default listener; it is unsuitable for an unselected private port.
In this run, preparation installed successfully but refused the occupied default
8080 listener before starting anything there; explicit activation recovered
cleanly. The commands below exercise the already prepared managed root.

```sh
.scratch/0.4.0-kapitsa/temper layout activate default \
  --root .scratch/0.4.0-kapitsa/managed-root --listen 127.0.0.1:18090
.scratch/0.4.0-kapitsa/temper stop --root .scratch/0.4.0-kapitsa/managed-root
.scratch/0.4.0-kapitsa/temper start --root .scratch/0.4.0-kapitsa/managed-root
```

Require the same layout/listener and calibration home after restart. Send one
16-token chat request. Repeat `start` and require the same router lifetime.
Stop, inspect absence of this private launchd job and owned listeners, then stop
again and require unchanged activation state. Keep both roots and all scratches.

Record one concise outcome in the release plan, including refusal/failure and
retry if any. Native observations belong to this integration check; do not
rewrite the historical research measurements or publish a catalog from it.
