# Guided setup catalog candidate

`guided-setup.json` is an unpublished authoring catalog for macOS ARM64. It offers two explicitly selected local main models and one compact utility profile for a harness-owned foreground model. The compact profile is useful for short field extraction, classification and bounded tool work; source fidelity and longer writing need review.

| Profile | User choice | Route |
|---|---|---|
| `qwen3.5-4b-local` | Compact local main | One `default` route, resident |
| `qwen3.5-4b-utility` | Local text utility beside an external foreground | One `available` route, loaded on demand |
| `qwen3.8-27b-q4xl-local` | Larger local main | Existing `default` route, resident |

Both models declare a **262,144-token ceiling** for the native input-plus-output
window. The official [Qwen3.8 27B](https://huggingface.co/Qwen/Qwen3.8-27B#best-practices)
and [Qwen3.5 4B](https://huggingface.co/Qwen/Qwen3.5-4B#processing-ultra-long-texts)
cards were checked on 23 September 2026. Their roughly 1M-token extensions need
RoPE scaling, which upstream recommends only for those longer inputs because
it can affect shorter sequences. Those extensions are separate configurations.
Temper's Context screen defaults to automatic selection from reviewed machine
findings. This candidate has no such findings yet: setup requests an explicit
window and shows its fit as unknown. `--context LAYOUT=TOKENS` supplies a window
up to the ceiling. The authored 32k/16k configurations remain available to
low-level compilation; they are not automatic machine recommendations.

```sh
./build/temper init --catalog catalog/guided-setup.json --dry-run
```

The published stable catalog and [`qwen38-m5-refresh.json`](qwen38-m5-refresh.json)
retain their original 32k configuration. That specimen reproduces the old
setup and protects issued execution identities; this candidate supplies the
new choices. Saved user choices are never silently expanded.

The Qwen3.5 choice is the exact `Qwen3.5-4B-Q4_K_M.gguf` file from `unsloth/Qwen3.5-4B-GGUF` at revision `e87f176479d0855a907a41277aca2f8ee7a09523`: **2,740,937,888 bytes**, SHA-256 `00fe7986ff5f6b463e62455821146049db6f9313603938a70800d1fb69ef11a4`, Apache-2.0. [Pinned upstream file](https://huggingface.co/unsloth/Qwen3.5-4B-GGUF/blob/e87f176479d0855a907a41277aca2f8ee7a09523/Qwen3.5-4B-Q4_K_M.gguf) and [official base-model license](https://huggingface.co/Qwen/Qwen3.5-4B) were checked on 23 September 2026. Upstream file metadata reports the same size and SHA-256; a bounded HEAD request confirmed availability without downloading weights.

The [Qwen3.5 4B assessment](../../v3/results/models/qwen3.5-4b/README.md) and its [retained result](../../v3/labs/workstreams/small-model-capability-portfolio/results/qwen3.5-4b.json) support the task description. Text-only Q4_K_M with thinking off and a 16,384-token context was measured on Apple M5 / 32 GiB using llama.cpp b10621. It completed short extraction, classification and tool workflows, but changed a supplied quantity during editing, returned a superseded appointment time, and left consequential false claims in an actual document revision. The suite used simulated tools and AI assessment; acceptable completed work and human time saved were not established. The utility profile makes the same model available to an external harness; it does not claim that offloading improves a complete job. The [archived investigation](../../v3/labs/workstreams/small-model-capability-portfolio/README.md) found regressions in a different helper composition.

This candidate reuses the exact llama.cpp b10964 and llama-swap v255 release records from [`qwen38-m5-refresh.json`](qwen38-m5-refresh.json). Its Qwen3.5 layout retains the assessed Q8 K/V cache, 256-token batches, thinking off, and temperature/top-k/top-p/presence-penalty settings; its authored seed and launch controls differ. The newer engine and exact authored configuration have **not** been measured with this model. Both compact profiles use a 0.50 `gpu_memory_utilization` preparation policy so the default macOS wired-memory prediction can consider smaller machines. That fraction is an authoring assumption for the budget check, not a llama.cpp memory limit or measured fit. Peak server RSS in the assessed 16k configuration was 6.15 GiB on M5 / 32 GiB; that is neither a minimum RAM requirement nor evidence of fit at 262k or on 16 GiB or other machines. Vision was not assessed. The [Qwen3.8 27B assessment](../../v3/results/models/qwen3.8-27b/README.md) likewise retains its exact 32k and 102,400-token observations; the declared ceiling does not extend those measurements to 262k.

Users must explicitly choose a profile before compilation or installation. A local catalog compile checks the authoring contract and produces an exact execution lock; it does not test model behavior, download weights, install software or publish this candidate. See the [catalog and execution-lock contract](../docs/contracts/execution-lock.md) for the compile command.

Each model's `description` is editable catalog text shared by its main and
utility profiles. Use Workshop's
[description editor](../../v3/workshop/README.md#edit-a-model-description), or:

```sh
./build/temper catalog describe --catalog catalog/guided-setup.json \
  --artifact qwen3.5-4b-q4km \
  --description 'Useful for my extraction tasks; I review its factual claims.' \
  --dry-run
```

Omit `--dry-run` to save the edit. `--description-file FILE` reads your wording
from a file; `--assessment-url URL` adds or replaces the optional link.
`--if-empty` offers a suggestion without replacing an existing description.
Descriptions do not change execution identity or measured context facts.
