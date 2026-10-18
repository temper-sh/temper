# Use specialists in a pipeline

Select a specialist in `temper configure`, include it in a layout, and activate
that layout. Use the selected preset ID as the request's `model`. Small CPU
rerankers can stay available; extraction and translation can load on demand.
The examples use the default local endpoint at `http://127.0.0.1:8080`.

## Extract structured records

NuExtract3 uses its native extraction template. Send the desired structure and
instructions in `chat_template_kwargs`; the user message contains the source.
The template describes field types, not expected answers.

```sh
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "nuextract3-q4km-llama",
    "messages": [{"role": "user", "content": "Order AB-42 belongs to Mira. It has not shipped."}],
    "chat_template_kwargs": {
      "template": "{\"records\":[{\"order_id\":\"string\",\"customer\":\"string\",\"shipped_at\":\"string\"}]}",
      "instructions": "Return each mentioned order, including orders awaiting dispatch. Use null for absent values. Do not invent dates.",
      "enable_thinking": false
    },
    "temperature": 0,
    "max_tokens": 256
  }'
```

For document images, use an OpenAI-style `image_url` content part with a data
URL alongside the task text. The preset installs and loads the matching
BF16 projector. Check record associations and missing fields in your application;
valid JSON alone does not establish factual correctness.
Prefer GPU for images: on the tested M5/32 GiB, the same invoice took about
3 seconds on GPU and 145 seconds on CPU. CPU remains useful for short text
requests when the GPU is needed elsewhere.

## Rerank search results

Run search first, then send its candidate passages to the reranker. Ettin is
the smaller English CPU option. Choose Qwen3 Reranker for multilingual work.
The index in each result refers to the original input position.

```sh
curl http://127.0.0.1:8080/v1/rerank \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "ettin-reranker-150m-q8-crispembed",
    "query": "When does the backup run?",
    "documents": [
      "The nightly backup starts at 02:00 UTC.",
      "Backups are stored in the secondary region."
    ],
    "top_n": 2
  }'
```

Use `qwen3-reranker-0.6b-q8-llama` to select the multilingual alternative.
Keep Qwen's embedded reranking template and pass the plain query without a
custom instruction prefix. Scores rank candidates within one request; they
are not calibrated confidence or directly comparable between models.
Ettin's fixed 7,999-token limit applies to each query/passage pair; Qwen's
authored window is 4,096 tokens. Chunk long sources before ranking them.

## Translate text

Choose a family and language focus in the
[specialist catalog](https://temper-sh.github.io/catalog/#specialist-recommendations).
Tiny Aya uses ordinary instructions; TranslateGemma uses language-code kwargs.

### Tiny Aya

Use Global for mixed language regions, Water for European and Asia-Pacific
languages, Fire for South Asian languages, or Earth for West Asian and African
languages. These are Cohere's regional emphases within the family's advertised
70+ languages. The [model cards](https://huggingface.co/CohereLabs/tiny-aya-global#model-details)
list coverage. All four use
[CC BY-NC 4.0 · Noncommercial](https://creativecommons.org/licenses/by-nc/4.0/).

Name both languages and include the source in a normal user message:

```sh
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "tiny-aya-water-q8-llama",
    "messages": [{"role": "user", "content": "Translate the following text from English into Swedish. Return only the translation.\n\nThe backup runs every Tuesday."}],
    "temperature": 0,
    "max_tokens": 256
  }'
```

The other IDs are `tiny-aya-global-q8-llama`, `tiny-aya-fire-q8-llama` and
`tiny-aya-earth-q8-llama`. Keep the embedded chat template. The official Q8_0
weights are 3.57 GB per model. GPU is the default; CPU is available but its
performance is unmeasured. The presets use a 4,096-token total context with
2,048 output tokens, greedy decoding, F16 KV, one slot, four CPU threads and
batch 512/microbatch 256. Larger windows up to the catalog's 8,192-token limit
need their own memory and workload check.

Global and Water have local translation measurements: about 1–2 seconds for
short requests, 22 seconds for a longer document and 3.78/3.79 GiB peak server
RSS on M5/32 GiB with llama.cpp b11396/Metal. Both changed operational meaning
in the longer document. Fire/Earth's regional quality recommendations follow
Cohere; they do not inherit these measurements. Both passed a GPU request check,
but Earth's Swahili answer changed Tuesday to Monday. The selected tuning preserves
the measured baseline; alternative settings have not been compared.

### TranslateGemma

TranslateGemma requires explicit source and target language codes. Use one
user message containing the text to translate and pass both codes through
`chat_template_kwargs`. Keep task instructions and system messages out of
the source text.

```sh
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "translategemma-4b-q5km-llama",
    "messages": [{"role": "user", "content": "The backup runs every Tuesday."}],
    "chat_template_kwargs": {
      "source_lang_code": "en",
      "target_lang_code": "sv"
    },
    "temperature": 0,
    "max_tokens": 256
  }'
```

The larger alternatives are `translategemma-12b-q5km-llama` and
`translategemma-27b-q4km-llama`. Keep input within the publisher's 2K-token
limit; the 4,096-token server window also reserves space for output.
Use valid language codes from the [official model card](https://huggingface.co/google/translategemma-4b-it#usage).
The current presets use the embedded publisher template with llama.cpp's native
text and language-code handling (b11485 or newer). Saved presets with
`translategemma-text-input-v1` retain their compatible adapter. Supply both
language codes on every request; omitted codes default to English for engine
initialization.
The presets use greedy decoding, F16 KV, one slot, four CPU threads,
a 512-token batch and 256-token microbatch. Three paired 4B checks found no
consistent benefit from microbatch 512, so 256 remains selected. This does not
establish optimal tuning for other sizes or workloads.
The selected family weights use the [Gemma license](https://ai.google.dev/gemma/terms).

On M5/32 GiB with llama.cpp b11396 and GPU, peak engine RSS was 3.00, 8.66 and
16.39 GiB for 4B, 12B and 27B. A longer translation took 20, 59 and 116 seconds.
These are process measurements, not minimum system RAM. CPU rendering is
supported, but CPU translation performance has not been measured.

Review the translated meaning before using it. The small local check found
merged UI actions in 4B, changed timing and negation in 12B, and changed
operational conditions in 27B. All sizes preserved some cases and failed
others; these model-assisted checks do not rank translation quality across
languages or replace bilingual review.

The publisher's [family report](https://arxiv.org/abs/2601.09012) compares
translation quality across sizes. It does not establish speed on your Mac
or the quality of the selected GGUF quantizations.
