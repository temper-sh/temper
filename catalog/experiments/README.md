# Qwen study compositions

`qwen-study.json` is the authoring catalog for Field Kit's prepared
`qwen-machine-study@4`. It is experimental and is not part of the published
stable channel. It includes Splash UD-Q4/Q5/Q6, Rapid MLX and vLLM Metal using
one shared MLX 4-bit revision, and llama.cpp UD-Q5/Q6/Q8. Field Kit owns which
compositions run on each RAM bucket and the frozen coding/context protocol.

Python supplies include exact CPython 3.12.13, all dependencies and an independent
13-package coding evaluator. The vLLM composition requires MLX-LM commit
`9e6acca691e64d6d8bb808c328fcdea459099cca` (0.32.0); replacing it with stable
0.31.3 does not reproduce this selection. Package installation is offline after
verified artifact download; participants never resolve dependencies.

## Authoring

`scripts/prepare-qwen-study.py` consumes three author-resolved pylock files named
`pylock.rapid.toml`, `pylock.vllm.toml` and `pylock.evaluator.toml`. Resolve on
macOS ARM64 with Python 3.12.13 and `MACOSX_DEPLOYMENT_TARGET=26.4`, using
`uv pip compile --python-version 3.12.13 --python-platform aarch64-apple-darwin
--format pylock.toml -o FILE INPUT`. Rapid and evaluator use `--only-binary :all:`.
For vLLM, all dependencies except the explicitly pinned MLX-LM source are wheels.
The reviewed top-level requirements are:

```text
rapid-mlx==0.15.2

vllm @ https://github.com/vllm-project/vllm/releases/download/v0.30.0/vllm-0.30.0%2Bcpu-cp312-cp312-macosx_11_0_arm64.whl
vllm-metal==0.30.0
mlx==0.32.1
mlx-lm @ git+https://github.com/ml-explore/mlx-lm@9e6acca691e64d6d8bb808c328fcdea459099cca
setuptools==77.0.3
wheel==0.45.1
```

The evaluator preserves asgiref 3.9.1, blinker 1.9.0, click 8.2.1, iniconfig
2.1.0, itsdangerous 2.2.0, Jinja2 3.1.6, MarkupSafe 3.0.2, packaging 25.0,
pluggy 1.6.0, Pygments 2.19.2, pytest 8.4.1, python-dotenv 1.1.1 and Werkzeug
3.1.3. Flask source comes from the experiment fixture, not an installed wheel.

```sh
python3 scripts/prepare-qwen-study.py \
  --temper-repo . \
  --closures /absolute/path/to/resolved-pylocks \
  --llama-lock /absolute/path/to/field-kit/catalog/packages/qwen-machine-study@3/execution.lock.json \
  --out catalog/experiments/qwen-study.json
```

This Python 3.11+ authoring script fetches metadata and the small pinned source
archive, never model weights. Review a regenerated closure as a change: resolving
again may select newer transitive versions. The committed catalog is the exact
installable authority. Recompile the Field Kit package with its authoring script
after accepting a catalog change.

Both Python environments installed and passed pip/CLI checks. All eight
profiles compile and render. These checks do not qualify model loading or fit;
eligible-machine inference remains pending. Q8 is a llama.cpp candidate with
separate compatibility and memory admission, not a Splash target.
