# Bounded native acceptance for managed layouts

Status: **LIFECYCLE CHECKS COMPLETED WITH FIXES — 2026-09-28**

This smoke verifies the new managed lifecycle on the owner's Apple M5 / 32 GiB.
It is separate from model quality, capacity research, release and live cutover.
The repository requires explicit authorization for runtime verification. The
implementation's Go tests, race tests, disposable-process test and terminal dry
run do not establish launchd/model compatibility.

## Observed result

The owner authorized the run on 28 September. The checks below completed across
bounded attempts on Apple M5 / 32 GiB, macOS 26.6.1, with a 24 GiB effective
Metal limit. Owned runtime totaled at most 419 seconds; request output
reservations totaled 352 of the 512-token allowance. All weights were reused
from the shared HF cache. Fresh preflight corrected template material to 66,460
bytes; the original estimate omitted Glimmer's 9,992-byte template.
The private job, engine lifetimes and listeners were
absent after cleanup; the second clean stop left activation state unchanged.
The live stack and system settings were not changed.

The fresh layout preflight refused Splash in startup: its declared 24 GiB cap
exceeds the 23 GiB startup allowance after Temper's reserve. The executed
`mixed` layout therefore preloaded Glimmer, retained Qwen/llama as default, and
left Splash on demand. Preset settings and memory limits stayed as listed below.
This verifies startup/default independence with that composition; it does not
qualify the originally proposed Splash startup set.

Two product defects were corrected during the smoke:

- llama-swap v260 omits `requests` in an empty, explicit in-flight snapshot.
  Temper initially rejected it, blocking readiness and normal stop. The parser
  now accepts that documented empty form while refusing missing snapshots,
  null arrays and malformed data. The exact producer shape is retained in the
  activity regression; see [the pinned producer](https://github.com/mostlygeek/llama-swap/blob/v260/internal/swaputil/events.go).
- An engine exited between process-table and kernel-identity reads during idle
  polling. Observation now retries the complete snapshot, at most three times,
  for `ESRCH` only. Permission and ownership failures still refuse. Hermetic
  regressions cover disappearance during executable, argv and listener reads.

Startup selection and repeat activation passed. Splash answered, unloaded after
five idle seconds, and answered again under a new recorded lifetime. The driver
used an incorrect timestamp-field name after that response; the reload was
confirmed from its exact record (`ps_lstart`, new PID, executable and argv),
then the remaining checks resumed without repeating those requests.
The default route used Qwen on b11157; named Glimmer requests used b11205.
Each load left only the selected exclusive group running. An active Glimmer
stream caused a switch refusal with byte-unchanged activation state; the stream
finished intact before the successful switch. The helper layout began unloaded
without a default alias, loaded on demand, and kept its running generation
through saved rename and deletion.

There are two operational limits to retain. The first cold Splash request hit
the 60-second deadline while its private cache was being converted; the run
aborted and stopped cleanly. Requests using the completed cache succeeded.
The final stop briefly refused a still-closing accepted connection and succeeded
on explicit retry. Neither case used a longer request limit or forced signals.

Full Go tests, vet and race checks pass after the fixes. This establishes the
listed lifecycle behavior on this machine, not model quality, full-context
capacity, a cold-start latency guarantee, release or live-cutover approval.

## Scope and preflight

The following is the original bounded procedure. The observed section above
records the startup adjustment and failures rather than rewriting that plan.

Use a newly created private root and loopback port **18088**, with one active
layout. Abort if the port is occupied or another experiment/model is running;
do not stop it. Recheck `temper machine facts`, disk and the configuration
preview immediately before preparation. Do not change sysctls.

Select only these exact catalog presets:

| Preset | Software | Smoke settings |
|---|---|---|
| `qwen3.8-27b-q4xl-splash` | Splash 1.1.0 | Explicit 32,768-token override, matching the prior short-request integration check; other settings retained |
| `qwen3.8-27b-q4xl-mtp` | llama.cpp b11157 | Authored 40,960-token window |
| `muse-glimmer-30b-q4xl-llama` | llama.cpp b11205 | Authored 57,344-token window |

All share llama-swap v260. These three exercise mixed engines and distinct
llama.cpp versions; Gemma 31B's exact retained configuration is already checked
by reconstruction and the catalog regression, so this smoke needs no new Gemma
download or model comparison.

The 28 September read-only preview found all selected weights in the shared HF
cache, **zero missing model bytes**, **115,587,575 bytes** of exact software
archives and **56,468 bytes** of template material. Preparation's remaining disk
allowance was **792,139,205 bytes**. Splash's first-start derived cache needs
roughly **21,407,997,279 additional bytes**, plus its **2 GiB** free-space reserve;
this runtime estimate is separate from preparation. Available disk was about
36.3 GB. Recheck these facts; abort rather than download missing weights or
increase this scope. Hashing cached weights and deriving the Splash tokenizer
are preparation work, not inference.

After authorization, start with this exact selection (substitute the newly
created absolute root for `SMOKE_ROOT`):

```sh
./build/temper configure --root SMOKE_ROOT --catalog catalog/guided-setup.json \
  --preset qwen3.8-27b-q4xl-splash \
  --context qwen3.8-27b-q4xl-splash=32768 \
  --preset qwen3.8-27b-q4xl-mtp \
  --preset muse-glimmer-30b-q4xl-llama --dry-run --json
```

Save only after the fresh preflight passes. Edit through `configure --show`,
then revision-checked `--file`, to add these layouts:

- `mixed`: all three included, only Splash in startup, Qwen/llama as default,
  `idle_seconds: 5`. The startup/default difference is intentional.
- `helper`: Glimmer included, empty startup, no default, `idle_seconds: 5`.

Prepare explicitly with `configure --resume --prepare`. This authorization may
cover the listed software/template downloads and isolated cache conversion;
it does not authorize missing model downloads. Stop on preparation failure.

## Bounded requests and transitions

Bound the serving portion to **10 minutes**, every HTTP call to **60 seconds**,
and total generated output to **512 tokens**. Use non-streaming 16-token
requests with `temperature: 0` and the prompt `Reply with the single word OK.`
except for the one busy-request check below. Check the selected process paths,
complete arguments, listener ownership and response model after every load.
Never signal a PID inferred only from an executable name.

1. Activate `mixed` on `127.0.0.1:18088`. Observe only the startup Splash process;
   the default must not imply Qwen/llama startup. Repeat activation and verify
   the same router lifetime is retained.
2. Request Splash by preset ID. Wait for its five-second idle eviction, observe
   absence as ordinary availability, request it again, and verify a new engine
   lifetime with a valid durable launch record.
3. Request `default`, then Glimmer by preset ID. Verify exact b11157/b11205 paths
   and completed eviction before the next exclusive group loads.
4. Start one streaming Glimmer request, at most 256 tokens: `List integers from
   one to fifty, one per line.` While activity is observed, attempt activation
   of `helper`. Require refusal, unchanged desired/current state and an intact
   request. Wait for it to finish; close the client, then switch successfully.
5. Confirm `helper` has no default alias and loads only when requested. Rename
   and then delete its saved composition through revision-checked edits; the
   frozen running generation must remain unchanged and stoppable.
6. Stop, inspect absence of the owned job, router, recorded engine groups and
   listener, and repeat stop. A second clean stop must create no effect.

Use the public commands throughout:

```sh
./build/temper layout activate mixed --root SMOKE_ROOT --listen 127.0.0.1:18088
./build/temper layout status --root SMOKE_ROOT --json
./build/temper layout activate helper --root SMOKE_ROOT --listen 127.0.0.1:18088
./build/temper layout stop --root SMOKE_ROOT
```

HTTP requests go only to `http://127.0.0.1:18088/v1/chat/completions`. A failed
readiness, identity, activity or time-bound check aborts further requests.
Recovery uses `layout status`, followed by `layout stop` or an explicit retry;
there is no force fallback. Keep the private root if ownership is unresolved.

## Cleanup and retained conclusion

Always attempt the owned stop after serving. Verify zero owned descendants and
closed listener before removing the new private root. Preserve the shared HF
cache and every pre-existing service, installation and experiment. Keep only a
concise outcome in the owning plan, or the smallest diagnostic needed to explain
a failure. Do not publish a service-support, memory-capacity or model-quality
claim from this smoke alone.
