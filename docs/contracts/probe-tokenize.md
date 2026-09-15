# `temper probe tokenize` — exact offline tokenization

Status: executable pre-release contract, introduced after alpha.6.

`probe tokenize` turns caller-supplied, already-rendered prompt bytes into one
canonical JSON array of token IDs. It exists so Field Kit can construct exact-N
inputs without discovering Temper installation paths or starting an inference
server. Temper selects no target, template, prompt, experiment, or next action.

## Invocation

```text
temper probe tokenize
  --root PATH
  --installation ID
  --layout ID
  [--software-lock software.lock.yaml]
  [--manifest manifest.yaml]
  [--lock manifest.lock.yaml]
  < rendered-prompt.bin
```

The input is limited to 64 MiB. Success writes only a compact canonical JSON
integer array and a newline to stdout. The command performs no generation and
uses no network.

## Admission

Before starting the tokenizer, Temper requires:

- a canonical software lock and matching installation receipt;
- a manifest and model lock that select the named layout;
- a GGUF `llama-server` layout and its receipt-verified immutable artifact set;
- the receipted `llama-cpp` selection and its regular executable
  `llama-tokenize` inside the named installation.

The subprocess receives only the macOS system `PATH` and runs the equivalent
of:

```text
llama-tokenize -m LOCKED_MODEL --stdin --ids --no-bos --offline --log-disable
```

Temper validates that stdout is exactly one JSON array containing only
non-negative integer IDs, rejects trailing values and oversized output, and
re-encodes the result canonically. Field Kit remains responsible for template
rendering, special-token policy, element-wise runtime equivalence, artifact
identity, and all grading.
