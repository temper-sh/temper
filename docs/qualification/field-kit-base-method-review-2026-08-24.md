# Field Kit base installation-method review — 2026-08-24

Status: **`release-artifact` selected after static and runtime review; concrete
adapter qualified hermetically and through the real isolated scratch
lifecycle**.

This review selects the macOS Apple Silicon installation method for the Field
Kit serving base. It does not create tested-version evidence or authorize
installation into a consumer Temper root. Both runtime passes used scratch
roots and processes and did not change the legacy live service.

## Decision boundary

Temper requires an automatic installation method to install the exact locked
closure or make no provider change. Resolution-time hashes are insufficient if
the later provider command is allowed to reinterpret a moving formula name.
The method must also expose a complete runtime closure, stage before one
commit, converge on a second run, and remove only paths whose receipt proves
Temper added.

The static disposition is:

| Package | `release-artifact` | `system-package` / Homebrew |
|---|---|---|
| `llama-swap` | **selected; adapter and real isolated lifecycle passed** | **reject for the Field Kit base** — the formula installs the same upstream archive and adds a shared tap/update boundary without changing the binary |
| `llama-cpp` | **selected; adapter and real isolated lifecycle passed** | **reject for the Field Kit base** — the formula is a different shared build whose install command cannot be bound to the complete exact closure in `software.lock.yaml` |

The first recipe policy is an exact release reviewed into each catalog
snapshot, not an installer-time `latest` lookup. A later catalog update may
adopt a newer upstream release only after refreshing the artifact identity and
re-running the applicable smoke gates. The lock then carries the exact release
tag, source revision, target asset locator, compressed/unpacked sizes,
installed-entry count, archive root, and SHA-256. The concrete schema and
adapter are implemented, and their real scratch gate passed. A shipping recipe
may now use these exact release identities; calling one `exact-tested` still
requires the separate reviewed-evidence join.

## Exact upstream snapshot reviewed

All upstream facts were refreshed on 2026-08-24. They are a dated review
input, not a moving catalog locator.

### `llama-swap`

- Latest release: [`v251`](https://github.com/mostlygeek/llama-swap/releases/tag/v251),
  commit `4ec317589b21f58b64802c2b3371a179b9fdaa53`.
- Darwin arm64 archive:
  `llama-swap_251_darwin_arm64.tar.gz`, 12,871,496 bytes,
  SHA-256 `b438acbfbe588b4a2e9ffe11f08eb22c5d7955b9b304cab0e668a8686edfccdc`.
- The release publishes a checksum manifest. GitHub's release-asset digest and
  the checksum manifest agree with a fresh local SHA-256 calculation.
- The archive contains `llama-swap`, `README.md`, and `LICENSE.md`. The arm64
  executable reports `v251 (4ec3175)`, has only macOS system/framework dynamic
  dependencies, and exposes the required config, listen, version, and
  watch-config flags. It is ad-hoc signed, not Developer ID signed or notarized.

The project's [Homebrew formula at the reviewed
revision](https://github.com/mostlygeek/homebrew-llama-swap/blob/1f091c992644bff4783abc4fd39a2d1613fb6235/Formula/llama-swap.rb)
uses that exact archive URL and SHA-256 and installs only the `llama-swap`
binary. Homebrew therefore supplies no different build or runtime closure for
this target. Direct isolated ownership is smaller and gives Temper an exact
directory-level commit and removal boundary.

### `llama-cpp`

- Latest stable release: [`v0.2.0`](https://github.com/ggml-org/llama.cpp/releases/tag/v0.2.0),
  annotated tag target `bb4caa7540188872173c44d161602d9271386413`.
- That stable release designates nightly build
  [`b10566`](https://github.com/ggml-org/llama.cpp/releases/tag/b10566); the
  build tag resolves to the same commit.
- Darwin arm64 archive:
  `llama-b10566-bin-macos-arm64.tar.gz`, 11,095,544 bytes,
  SHA-256 `533f546dab2ce2f8e29ce3070f26acc55acc59528e177f2cd0d52b7f69b44f50`.
  GitHub publishes an [artifact
  attestation](https://github.com/ggml-org/llama.cpp/attestations/42207505)
  binding this digest to the source repository and commit.
- The extracted archive is about 26 MiB. It contains `llama-server`, the other
  upstream tools, the MIT license, and a self-contained `@loader_path` dylib
  closure including ggml 0.21.0 and the Metal backend. `llama-server` loads
  from the extracted directory, reports `0.2.0-dev` / build 10566 / commit
  `bb4caa754`, and exposes the Field Kit profile flags checked in this pass.
  Its Metal dylib links the system Metal, MetalKit, and Foundation frameworks.
  The executables are ad-hoc signed, not Developer ID signed or notarized.

The reviewed [Homebrew `llama.cpp`
formula](https://github.com/Homebrew/homebrew-core/blob/1c4ee07d442b5df40d490907dfc569125accc106/Formula/l/llama.cpp.rb)
builds stable `v0.2.0` from the same commit. Its Tahoe arm64 root bottle is
7,795,291 bytes with SHA-256
`b70f377b407c18b71b6268cb182042df851b49a401dd616a3c265ab16b7adfdc`.
Unlike the upstream archive, it deliberately uses shared Homebrew `ggml` and
`openssl@3`; the resolved runtime closure also reaches `libomp` and
`ca-certificates`. The root bottle inspected here is about 19 MiB after
extraction, before those shared dependencies.

## Gate assessment

| Gate | Upstream release artifacts | Homebrew variants |
|---|---|---|
| Immutable input | Passes static review: exact release/tag, target asset, SHA-256, and licenses are available; `llama-cpp` also has a GitHub build attestation | Bottle/archive hashes are available at resolution time |
| Exact install | Feasible: download exact locked locators, verify before extraction, stage the complete group, validate, then rename one installation directory | Fails the automatic-method contract: Homebrew installs formula names against the newest metadata it knows; its documented no-upgrade controls are not a lock and dependencies may still move |
| Runtime contents | `llama-swap` is one system-linked executable; `llama-cpp` carries its executable/dylib/Metal closure under one root | `llama-swap` is byte-identical to upstream; `llama-cpp` has a shared five-formula closure on the reviewed snapshot |
| Metal and profile flags | Passes the bounded runtime gate: the reviewed b10566 server loaded the frozen profile and offloaded 29/29 layers to the Apple M5 Metal device | The retained b10470 Homebrew binary supplied the read-only compatibility baseline, but that does not make its installation method exact |
| Performance | Passes as an adoption-regression screen: correct work completed in every CPU and Metal run; the small timing sample is not a general performance claim | Compared only as the already-installed retained baseline; no provider install or mutation occurred |
| Update | Feasible as a new staged immutable group plus one pointer/receipt commit; no in-place overwrite | Formula metadata, shared dependencies, linking, cleanup, and other Homebrew actors are outside Temper's single commit |
| Removal | Feasible by deleting only the exact receipted isolated group after claims are released | Shared racks, opt links, retained old kegs, dependencies, and other dependents require Homebrew-global authority; this adds risk without a Field Kit benefit |

Homebrew documents that [`brew bundle` has no lock-file
semantics](https://docs.brew.sh/Brew-Bundle-and-Brewfile), and its [version
guidance](https://docs.brew.sh/Versions.html) says `--no-upgrade` does not pin
versions and `brew install` may still upgrade dependencies. A private frozen
tap could add a new ownership system, but then
Temper would maintain downstream formulae and still share Homebrew's global
effect boundary. That is more machinery than the verified isolated artifacts
require and is not selected.

This rejection is package-specific. It does not revisit the owner's approved
small shared Homebrew bootstrap layer for `uv` and `hf`; those packages have
their own qualification and exact-effect obligations.

## Runtime qualification — 2026-08-24

The authorized smoke used only already-downloaded, hash-verified release
artifacts and an already-cached
`Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp` Q8_0 model. The exact model blob
SHA-256 was
`6ddab39a36c6c87fdb76f0e5f05657012d5dbc97034c0983c157f17ef9f34d55`.
All processes listened on scratch loopback ports, ran with offline model
loading, and were stopped after the checks. Preflight and postflight both found
the legacy router on `127.0.0.1:8080` healthy with no loaded model; it was not
stopped, reconfigured, or signalled.

The direct-server matrix used the retained Homebrew b10470 executable as the
compatibility baseline and the reviewed upstream b10566 executable as the
candidate. Each case loaded the same model with the retained profile
(`ctx-size=4096`, one parallel slot, batch and ubatch 256), then completed the
same frozen `/v1/rerank` request twice. CPU used `-ngl 0`; Metal used `-ngl
99`. Every response was HTTP 200 with three results, the expected document at
rank one, and an identical repeat score within that backend.

| Server | Backend | Startup (s) | Request 1 / 2 (s) | Prompt tok/s | Peak RSS (KiB) | Outcome |
|---|---:|---:|---:|---:|---:|---|
| Homebrew b10470 | CPU | 0.654 | 0.615 / 0.581 | 543 | 2,394,032 | pass |
| Upstream b10566 | CPU | 0.647 | 0.435 / 0.426 | 755 | 2,388,400 | pass |
| Homebrew b10470 | Metal | 0.433 | 0.120 / 0.077 | 3,327 | 1,262,944 | pass |
| Upstream b10566 | Metal | 0.435 | 0.176 / 0.082 | 2,528 | 1,257,760 | pass |

The candidate was 27–29% faster in the two tiny CPU requests. Its Metal cold
request was 0.056 seconds slower and its warm request 0.005 seconds slower;
startup and peak RSS were effectively unchanged. Backend scores differed
slightly while ranking and repeatability did not. This sample is deliberately
only an adoption-regression screen: execution order, cache state, and the
b10470-to-b10566 source change prevent a general speed claim or a pure
packaging A/B. A verbose candidate probe separately proved Metal device
selection, 29/29-layer offload, kernel loading, and clean Metal deallocation.

The v251 upstream `llama-swap` artifact then started on `127.0.0.1:19081`
with a temporary config whose model command named the exact b10566 executable
and cached GGUF. Model discovery succeeded, one routed rerank returned HTTP 200
with the expected top document, and the child appeared in router state. A
deliberately cancelled client request reached the router cancellation/499 path.
Touching the watched scratch config caused a reload and clean child exit. The
router then shut down cleanly. Peak RSS was 44,048 KiB for the router and
2,521,600 KiB for its CPU child. Socket inspection found only the declared
loopback listeners and no external connection.

## Real isolated adapter lifecycle — 2026-08-24

Immediately before the run, the official release APIs still named v251 and
stable v0.2.0 / build b10566 as current. Fresh downloads reproduced both
published SHA-256 digests. The exact archive manifests used by the lock were:

| Unit | Compressed bytes | Regular-file bytes | Installed entries | Archive root |
|---|---:|---:|---:|---|
| `upstream-release:llama-swap` | 12,871,496 | 23,027,581 | 3 | `.` |
| `upstream-release:llama-cpp` | 11,095,544 | 27,555,366 | 61 | `llama-b10566` |

The complete public command edge then consumed one direct-experiment
`temper-software-lock/v1` in a new root on exact target
`darwin/arm64/macos/26.6.1`:

1. `software install --dry-run` reported two isolated publishes and left the
   root absent.
2. The first install downloaded, hashed, bounded, extracted, inspected, and
   published both scopes. The installed executables reported v251 commit
   `4ec3175` and llama.cpp build 10566 commit `bb4caa754`.
3. `software check` reported both units exact. The second install reported
   `unchanged`; receipt and root-state bytes were unchanged.
4. `software remove --dry-run` left receipt and root-state bytes unchanged.
   The first removal released both Temper-added scopes, the second reported
   `unchanged`, and a final read-only check reported the two expected
   `provider-missing` findings.

The concrete `upstream-release` reader/installer therefore has hermetic
archive-path, symlink, bounds, atomic-publication, repair, and scope-removal
coverage plus a real network-backed lifecycle through C11. This gate selects
the method and permits exact recipes for these artifacts. It does not
manufacture an `exact-tested` catalog row; stable Results or Field Kit evidence
must still supply the reviewed installed-base proof for that status.
