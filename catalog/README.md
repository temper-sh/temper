# Choose a preset

Temper pairs each model with weights chosen for quality, an engine suited to
the model and hardware, and tuned settings. Choose a preset for the work you
want to do and the resources you can spare.

Open `temper init` for a new setup or `temper configure` to change one.
The wizard shows the requirements and estimated memory use before you prepare
anything. [Follow the setup guide](../docs/contracts/init.md) to get a model
running in your app.

## Recommended presets

| Preset | Useful for | Main tradeoff |
|---|---|---|
| Qwen3.8 27B with Splash | Coding and proposing changes to a repository | Faster first patch submissions in the tested comparison; patches still needed review. Requires Apple M3 or newer and macOS 26.4+. |
| Muse Glimmer 30B | Technical explanations, drafts, planning, and a second review | Useful structure and explanations; database and rollout advice needed substantive corrections. |
| Gemma 4 26B-A4B | Creative drafting, dialogue, and general chat | Shorter waits than 31B on the tested M5; facts and narrative continuity still need checking. |
| Gemma 4 31B | Creative drafting and revision | Much longer waits on the tested M5; the small writing comparison did not establish a human preference advantage over 26B. |
| Qwen3.8 27B with llama.cpp | Coding, combining sources, and document work | An alternative engine for Qwen; long inputs can be slow, and strict formats still need checking. |

The Qwen choices use the same target weights with different engines and
settings. Installing both reuses shared model files. Splash's first startup
also creates a weight cache, which adds time and disk use.

These are recommendations for particular uses, not a ranking of every model.
We combine trusted upstream and practitioner findings with our own experiments,
compatibility checks, and measurements. The
[research and guides](https://github.com/temper-sh/temper-sh.github.io)
summarize the assessments and their limits.

## Smaller models

The **All** tab includes the recommendations above and these smaller choices.
A smaller model can be your main assistant or a helper alongside another model.

| Preset | Useful for | Main limitation observed |
|---|---|---|
| Gemma 4 E2B | Extracting fields from short records and simple tool tasks | Some writing changed facts; recovery from a conflicting edit stopped before saving. |
| Gemma 4 E4B | Short drafts from supplied facts and simple tool workflows | Longer editing retained incorrect claims after feedback. |
| Qwen3.5 4B | Extraction, classification, and short tool workflows | Longer writing, translation, and editing need close checking. |

## Check what fits

Most local measurements come from an Apple M5 with 32 GiB. Fit on actual
8 or 16 GB Macs remains unverified. Memory tiers in the chooser are estimates,
and a model's file size is only part of its memory use.

Longer conversations, larger documents, and several loaded models need more
memory. Start with the preset's context setting and review the layout's
startup estimate. Selecting several alternatives does not require loading them
together.

Current presets default to medium thinking. Some retained measurements used
thinking off or different software; they describe those tested configurations
and do not establish performance for every current setting.

## Keep or change a choice

Your saved presets stay fixed when the catalog changes. Use
`temper configure` to make a deliberate change, review any new downloads, and
activate the layout when you are ready.

- [Set up and manage layouts](../docs/contracts/init.md)
- [Update or inspect the catalog](../docs/CATALOG.md)
- [Author a preset or compile exact inputs](../docs/contracts/execution-lock.md)
