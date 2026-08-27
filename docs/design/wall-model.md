# Bootstrap wall-model prediction

Status: executable design, 2026-08-20.

The wall model answers one narrow question for the mode selected by
`temper check`: does the predicted GPU-resident posture fit below this
machine's Metal wired-memory wall? It is an admission prediction, not a
measurement, qualification witness, or promise that a workload will remain
stable.

## Facts and ownership

No new persistent field is added for this slice.

- `manifest.defaults.gpu_memory_utilization` is the user's conservative
  allocation policy for the local foreground budget holder. It is an input to
  this prediction; v1 does not pass it to llama-server or present it as a
  measured footprint.
- Physical memory and the live wired limit are machine reads performed for
  each check. They are not copied into the manifest or lock.
- Selected model-byte sizes come from the already verified immutable artifact
  receipts. They are lower bounds on runtime footprint, not evidence claims.
- Reviewed runtime footprints and machine applicability remain qualification
  catalog facts. The bootstrap prediction cannot manufacture them from a file
  size.

This preserves one home per fact: intent in the manifest, resolution in the
lock, local materialization facts in receipts, live hardware at the read edge,
and reviewed measurements in the future qualification catalog.

## Arithmetic

All byte quantities are converted to MiB and integer results round toward the
safer side: model bytes round up; percentage-derived capacities and allocation
round down.

The bootstrap hardware adapter uses the witnessed legacy approximations until a
native Metal capability read replaces them:

```text
device_mib = floor(physical_mib × 0.81)
wired_mib  = positive live iogpu.wired_limit_mb
             or floor(physical_mib × 0.65) when macOS reports no override
os_floor_mib = 1024
```

`0.81` is the predicted Metal recommended working-set share. `0.65` is the
predicted macOS default wired wall when no live override is exposed. The
1,024 MiB OS/transient floor is policy, not a measurement. Every output names
whether the wired wall was live or predicted.

For a local-foreground mode, the preferred resident coder is the budget
holder. A member with `ngl: 0` is CPU-only and does not enter the GPU sum.
The holder envelope and required total are:

```text
fraction_envelope_mib = floor(gpu_memory_utilization × device_mib)
holder_mib            = max(fraction_envelope_mib,
                            ceil(holder model bytes / MiB))
co_tenants_mib         = sum(ceil(model bytes / MiB)) for every other
                         GPU-resident member
required_mib           = holder_mib + co_tenants_mib + os_floor_mib
fits                    = required_mib <= wired_mib
```

Counting the complete model artifact for a partially offloaded co-tenant is
deliberately conservative: v1 has no reviewed tensor-placement footprint from
which to claim a smaller number. On-demand members are not part of idle
residency and do not enter this gate. Their overlap risk belongs in a later
catalog-backed prediction once measured runtime profiles exist.

An `off` mode, or a local foreground explicitly placed at `ngl: 0`, is
`not-applicable`. If a GPU-resident artifact cannot pass routine admission,
the budget is `unavailable`; the artifact finding already explains why and no
second synthetic problem is added. If arithmetic exceeds the wall, check adds
one `budget-exceeded` finding on the holder layout and reports a conservatively
rounded utilization fraction that fits when lowering the fraction can solve
it.

## Boundary

The arithmetic is a pure package over typed inputs. Hardware probing is a
separate read adapter, artifact inspection is the existing artifact-set read,
and `check` only orchestrates them. No branch writes, repairs, downloads,
changes the wired limit, invokes sudo, or contacts the network.
