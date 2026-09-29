# Choose a catalog preset

A preset fixes weights, serving software, templates and settings. A layout
combines your selected presets and independently chooses startup loading and an
optional default. [The authoring guide](../catalog/README.md) describes the
available presets and their evidence.

Build the current source and review explicit choices:

```sh
go build -o build/temper ./cmd/temper
./build/temper init --catalog catalog/guided-setup.json --dry-run
```

Nothing starts selected. Save retains exact choices in `configuration.json`;
Prepare installs missing material. [The setup guide](contracts/init.md) covers
later edits, offline reuse, and explicit activation.

## Compile an independent execution

```sh
./build/temper catalog compile --catalog catalog/guided-setup.json \
  --preset qwen3.8-27b-q4xl-mtp --target darwin/arm64 \
  --out execution.lock.json
./build/temper execution inspect --lock execution.lock.json
```

The lock owns everything needed to reproduce the selected execution. Compilation
works offline with recorded software, supports `--dry-run`, and leaves identical
outputs unchanged. A different configuration needs a new output path. Continue
with [installation and execution](contracts/execution-runtime.md) after reviewing
the downloads and requirements.

`--software latest` explicitly discovers and verifies newer release archives.
`--software tested` requires a recorded tested version and evidence. Both must
satisfy required versions; failures never select a fallback automatically.
[Version and identity rules](contracts/execution-lock.md) explain the boundaries.

## Signed catalog updates

Current source accepts only catalog v3 and execution-lock v3. Signed sequence 3
is prepared in `docs/catalog` for alpha.11. The live channel still serves v2
until that commit is deployed; use the explicit authoring catalog until then.

Once a v3 catalog is published:

```sh
temper catalog update --root "$HOME/.temper"
temper catalog inspect --root "$HOME/.temper" --json
temper catalog compile --root "$HOME/.temper" \
  --preset qwen3.8-27b-q4xl-mtp --target darwin/arm64 \
  --out execution.lock.json
```

Catalog updates leave saved choices and installations unchanged. Inspection
lists retained snapshots. `catalog rollback --root ROOT --snapshot SHA256`
selects an exact retained v3 snapshot offline. The store remembers the highest
accepted sequence, so an older network response cannot silently roll it back.
See the [distribution contract](contracts/catalog-distribution.md).

Older catalogs, Selection files and saved execution locks need fresh compilation
and configuration in a fresh root, such as `--root "$HOME/.temper-alpha11"`.
Historical experiments keep their pinned older host; current Temper does not
migrate their catalog stores or configuration.
