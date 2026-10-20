# Harness model integration

Choose Pi, PiG or Hermes on the **Harnesses** screen in `init` or `configure`.
Detection never selects an integration. Review shows the exact configuration
destination; saving records that destination but does not write it. Each later
layout activation synchronizes the included chat models, including on-demand
models, from their saved locks and the actual listener.

Configuration has an optional `integrations` array:

```json
{
  "integrations": [
    {"id": "pi", "models_file": "/Users/alex/.pi/agent/models.json"},
    {"id": "pig", "models_file": "/Users/alex/.pig/agent/models.json"},
    {"id": "hermes", "models_file": "/Users/alex/.hermes/config.yaml"}
  ]
}
```

Omission or an empty array means none. Paths are absolute and reviewed when
selected; later environment changes do not redirect writes. Discovery follows
`PI_CODING_AGENT_DIR` for Pi and `PIG_CODING_AGENT_DIR`, `PIG_HOME`,
`XDG_CONFIG_HOME` and `PIG_USE_PI_DIRS=1` for PiG. Existing selections retain
their saved destinations. Shared destinations are merged once and are visible
to both Pi/PiG readers, even if only one reader was selected. A Hermes YAML
destination cannot also be a Pi/PiG JSON destination. The `models_file` field
names the provider/model configuration file for each client.

## Pi and PiG

The provider key is `temper`, stable across layouts and visible in Pi's model
picker and footer. Its name identifies the absolute Temper root and is its
ownership marker. Each `models.json` can therefore integrate one Temper root.
An existing `temper` provider without that ownership marker, including one
owned by another root, is refused.
Temper replaces the owned provider's endpoint and model list (including removing
stale per-model transport overrides), preserving
unrelated providers, unknown fields and user authentication/settings. Stop
retains entries; deselection stops synchronization. To remove an integration's
entries, deselect it and explicitly remove that provider from `models.json`.
Existing generic `local` providers are never adopted.

Models use their routed preset IDs, display names, per-slot context and text/image
modalities. Non-chat interfaces are excluded. The router's `default` alias is
not duplicated in the picker. Thinking remains controlled by the saved server
preset; export does not introduce a client reasoning override. Output metadata
uses the explicit output default, or context capacity when it is omitted;
uncapped presets suppress client token-limit defaults in request sampling
parameters. Harness session/default-model selection, compaction and credentials
remain harness-owned. Open `/model` to reload Pi's models; restart PiG when its
running picker has not refreshed.

## Hermes

Discovery looks for `hermes` on PATH. It resolves `HERMES_HOME` (including
home/environment expansion) and the sticky `active_profile` selection, falling
back to `~/.hermes/config.yaml` when neither is set. An explicit
`HERMES_HOME` pointing to `profiles/<name>` selects that profile directly.
The reviewed absolute path is persisted; changing profiles afterward does not
redirect synchronization. To target a different profile, explicitly configure
its destination with `configure --file`.

Temper adds a named `providers.temper` entry to that profile's `config.yaml`,
using the active listener and `chat_completions` transport. Its YAML comment
`temper-root: "<absolute root>"` records ownership without changing the visible
provider name or adding unsupported Hermes settings. Keep this comment:
an existing entry with a missing or different marker is refused. A legacy
`custom_providers` entry named `temper` is also refused; other legacy entries
are retained.

The `models` mapping supplies per-model `context_length` and `supports_vision`
from saved chat presets. Automatic discovery stays enabled, so Hermes can also
show the router's aliases and non-chat routes. Filtering those entries is
deferred. Server presets continue to control thinking and output defaults;
an existing global Hermes context override remains authoritative.

Other providers, credentials, default models, auxiliary settings and unknown
fields survive. YAML comments and values are retained, although formatting may
change when publishing an update. Configuration mappings require string keys.
Duplicate keys and aliases, anchors or merge keys on managed mappings are refused before activation;
unrelated YAML anchors and aliases are preserved. Stale owned model metadata
is removed when the active layout changes.

Start a new Hermes session after the first synchronization, then choose
`temper` in its model picker. Temper does not switch the default provider,
restart Hermes or change an active session.

## Activation and recovery

Stopping retains the provider; deselection stops synchronization. To remove an
integration's entries, deselect it and explicitly remove `providers.temper`
from the reviewed configuration file.

Activation first reads and validates all destinations and proposed merges.
After service readiness, it rechecks each snapshot and atomically replaces each
changed file. Cooperating Temper writers serialize at the destination; detected
external edits are refused, requiring review and another activation. An editor
that ignores the lock can still race the final comparison/rename; stop editing
the same file during activation. Symlink destinations are resolved for review;
the resolved path is saved, and redirection of that saved path is refused.
Changing the original alias requires selecting the new destination explicitly.
Hard-linked destinations are refused.

Service and integration outcomes are reported separately. A post-activation
file failure returns a nonzero result with the service still running. Repeating
activation repairs drift or partial writes without restarting an unchanged
service. Separate files and launchd do not share a transaction. An identical
activation preserves file bytes and timestamps. Dry run creates no directories,
writes no files and starts no service. Status remains read-only.

This configuration requires Temper 0.4.0 once integrations are saved; older
binaries reject the new field rather than silently ignoring the selection.
Existing configurations without integrations continue to work.

Adapter references checked on 2026-10-20:
[Pi model configuration](https://github.com/earendil-works/pi/blob/48c8f322844087ff6cd80885f093ad44557647d1/packages/coding-agent/docs/models-configuration.md),
[Pi configuration](https://github.com/earendil-works/pi/blob/48c8f322844087ff6cd80885f093ad44557647d1/packages/coding-agent/docs/configuration.md),
[PiG configuration](https://pi-in-go.dev/docs/latest/configuration/),
[PiG providers](https://www.pi-in-go.dev/docs/latest/custom-provider/),
[Hermes provider configuration](https://github.com/NousResearch/hermes-agent/blob/bd3cbe91a90097f9804f16ea5c1768f5cbaaf0b7/hermes_cli/config_providers.py),
[Hermes model discovery](https://github.com/NousResearch/hermes-agent/blob/bd3cbe91a90097f9804f16ea5c1768f5cbaaf0b7/hermes_cli/model_switch.py),
and [Hermes profile resolution](https://github.com/NousResearch/hermes-agent/blob/bd3cbe91a90097f9804f16ea5c1768f5cbaaf0b7/hermes_cli/main.py).
