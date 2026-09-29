#!/usr/bin/env python3
"""Refresh the current preset study catalog from uv pylocks; never fetch weights.

Requires Python 3.11+. Resolve the input pylocks for CPython 3.12.13,
aarch64-apple-darwin, MACOSX_DEPLOYMENT_TARGET=26.4. This authoring command
fetches metadata and the small MLX-LM source archive only.
"""
import argparse
import copy
import hashlib
import io
import json
from pathlib import Path
import re
import tarfile
import tomllib
import urllib.request

TARGET = {"os":"darwin", "arch":"arm64"}
MLX_REVISION = "10c35caafbb80f7dc6a7a432cdd11af10a6d4818"
GGUF_REVISION = "4ca720788d1e01f1bff70c033e0d0028fd02e502"

def read_url(url, limit=32*1024**2):
    with urllib.request.urlopen(url, timeout=60) as response:
        data = response.read(limit+1)
    if len(data)>limit: raise ValueError("metadata/source byte ceiling exceeded")
    return data

def remote_json(url):
    return json.loads(read_url(url))

def write(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2)+"\n")

def supply(name, path, runtime):
    rows=[]
    for row in tomllib.loads(path.read_text())["packages"]:
        if row.get("vcs"):
            source=row["vcs"]
            commit=source["commit-id"]
            repo=source["url"].removeprefix("https://github.com/")
            url="https://codeload.github.com/"+repo+"/tar.gz/"+commit
            raw=read_url(url,64*1024**2)
            with tarfile.open(fileobj=io.BytesIO(raw),mode="r:gz") as archive:
                members=archive.getmembers()
                artifact={"locator":url,"sha256":hashlib.sha256(raw).hexdigest(),"size":len(raw),
                          "format":"tar.gz","archive_root":repo.split("/")[1]+"-"+commit,
                          "installed_entries":len(members),"unpacked_size":sum(m.size for m in members if m.isfile())}
            rows.append({"name":row["name"],"version":row["version"],"revision":commit,"artifact":artifact})
            continue
        wheels=row.get("wheels") or [row["archive"]]
        eligible=[]
        for wheel in wheels:
            filename=wheel["url"].split("/")[-1].replace("%2B","+")
            if filename.endswith("-none-any.whl") and re.search(r"-(?:py2\.)?py3(?:[2-9]|10|11|12)?-none-",filename):
                eligible.append(wheel)
            elif ("-py3-none-" in filename or "-cp312-cp312-" in filename or re.search(r"-cp3(?:[2-9]|10|11|12)-abi3-",filename)) and ("arm64.whl" in filename or "universal2.whl" in filename):
                eligible.append(wheel)
        if not eligible: raise ValueError("no CPython 3.12 macOS wheel: "+row["name"])
        def wheel_rank(wheel):
            filename=wheel["url"].split("/")[-1]
            version=re.search(r"macosx_(\d+)_(\d+)",filename)
            deployment=tuple(map(int,version.groups())) if version else (0,0)
            if deployment>(26,4): raise ValueError("wheel exceeds the study macOS minimum")
            return ("arm64.whl" in filename,"-cp312-cp312-" in filename,deployment,filename)
        wheel=max(eligible,key=wheel_rank)
        size=wheel.get("size")
        if size is None:
            with urllib.request.urlopen(urllib.request.Request(wheel["url"],method="HEAD"),timeout=60) as response:
                size=int(response.headers["Content-Length"])
        rows.append({"name":row["name"],"version":row["version"],
                     "artifact":{"locator":wheel["url"],"sha256":wheel["hashes"]["sha256"],"size":size}})
    return {"package":name,"target":TARGET,"python":{"root":"pytest" if name=="coding-evaluator" else name,
            "runtime":runtime,"packages":rows}}

def hf_artifact(repo, revision, format, names=None):
    tree=remote_json("https://huggingface.co/api/models/"+repo+"/tree/"+revision+"?recursive=true")
    files=[]
    for row in tree:
        name=row["path"]
        if row["type"]!="file" or names is not None and name not in names: continue
        if names is None and name in (".gitattributes","README.md"): continue
        sha=row.get("lfs",{}).get("oid")
        if not sha:
            raw=read_url("https://huggingface.co/"+repo+"/resolve/"+revision+"/"+name)
            sha=hashlib.sha256(raw).hexdigest()
        files.append({"path":name,"bytes":row["size"],"sha256":sha})
    return {"repo":repo,"revision":revision,"files":sorted(files,key=lambda f:f["path"]),"format":format,"license":"Apache-2.0"}

def main(args):
    repo=args.temper_repo
    d=json.loads((repo/"catalog/experiments/qwen-study.json").read_text())
    if d["schema"] != "temper-catalog/v3":
        raise ValueError("Qwen authoring requires the current preset catalog")
    d.pop("preset_order",None)
    for key in list(d):
        if key not in ("schema","date","runtime","artifacts","patches","engines","presets"):
            d.pop(key)
    metadata=remote_json("https://raw.githubusercontent.com/astral-sh/uv/0.12.5/crates/uv-python/download-metadata.json")["cpython-3.12.13-darwin-aarch64-none"]
    runtime={"name":"cpython","version":"3.12.13","revision":"python-build-standalone:"+metadata["build"],
             "artifact":{"locator":metadata["url"],"sha256":metadata["sha256"]}}
    d["runtime"]["python_environments"]=[supply("coding-evaluator",args.closures/"pylock.evaluator.toml",runtime)]
    llama_preset=copy.deepcopy(d["presets"]["llama-q5"])
    selected_engines={d["presets"]["splash-q4"]["engine"], llama_preset["engine"]}
    d["engines"]={k:v for k,v in d["engines"].items() if k in selected_engines}
    for family,filename in [("rapid-mlx","rapid"),("vllm-metal","vllm")]:
        d["engines"][family]={"family":family,"adapter":family+"/v1","interfaces":["chat-completions"],"modalities":["text"],
                              "supply":supply(family,args.closures/("pylock."+filename+".toml"),runtime)}
    splash=copy.deepcopy(d["presets"]["splash-q4"])
    llama_base=llama_preset
    base_artifact=splash["artifact"]
    draft=splash["speculation"]["draft_artifact"]
    d["artifacts"]={key:value for key,value in d["artifacts"].items() if key in (base_artifact,draft)}
    d["patches"]={key:value for key,value in d["patches"].items() if key in splash["patches"]}
    for quant in ("q5","q6","q8"):
        name="Qwen3.8-27B-UD-"+quant.upper()+"_K_XL.gguf"
        d["artifacts"]["qwen-"+quant]=hf_artifact("unsloth/Qwen3.8-27B-GGUF",GGUF_REVISION,"gguf",[name])
    d["artifacts"]["qwen-mlx"]=hf_artifact("mlx-community/Qwen3.8-27B-4bit",MLX_REVISION,"mlx-safetensors")
    d["artifacts"]["qwen-vllm"]=copy.deepcopy(d["artifacts"]["qwen-mlx"])
    d["artifacts"]["qwen-vllm"]["format"]="safetensors"
    for patch in d["patches"].values():
        patch["compatible_artifacts"]=[base_artifact,"qwen-q5","qwen-q6","qwen-q8"]
    d["presets"]={}
    specs=[("splash-q4",splash,base_artifact),("splash-q5",splash,"qwen-q5"),("splash-q6",splash,"qwen-q6"),
           ("llama-q5",llama_base,"qwen-q5"),("llama-q6",llama_base,"qwen-q6"),("llama-q8",llama_base,"qwen-q8")]
    for identity,base,artifact in specs:
        preset=copy.deepcopy(base)
        preset.update(display_name=identity,artifact=artifact,context_window_tokens=118000,context_limit_tokens=262144)
        preset["recommended"]=False
        preset.pop("context_findings",None)
        preset.pop("engine_versions",None)
        preset["patches"]=splash["patches"]
        preset["request_defaults"]["max_output_tokens"]=100000
        if identity.startswith("splash"):
            preset["engine_config"].update(max_memory_bytes=27*1024**3,request_timeout_seconds=14400)
        d["presets"][identity]=preset
    for family,artifact in [("rapid-mlx","qwen-mlx"),("vllm-metal","qwen-vllm")]:
        preset=copy.deepcopy(d["presets"]["splash-q4"])
        preset.update(display_name=family,artifact=artifact,engine=family,patches=[],speculation={"method":"none","source":"none","max_draft_tokens":0})
        if family=="rapid-mlx":
            tuning={"max_num_seqs":1,"max_concurrent_requests":1,"prefill_batch_size":1,"completion_batch_size":1,
                    "gpu_memory_utilization":0.75,"prefix_cache":"off","kv_cache_dtype":"bf16","pflash":"off",
                    "reasoning_parser":"qwen3","tool_call_parser":"qwen3_coder_xml","prefill_step_size":1024,"request_timeout_seconds":14400}
        else:
            tuning={"max_num_seqs":1,"max_num_batched_tokens":1024,"gpu_memory_utilization":0.75,"prefix_cache":"off",
                    "kv_cache_dtype":"auto","reasoning_parser":"qwen3","tool_call_parser":"qwen3_coder",
                    "language_model_only":True,"chunked_prefill":True,"block_size":16}
        preset["engine_config"]={"kind":family+"/v1",family.replace("-","_"):tuning}
        d["presets"][family]=preset
    write(args.out,d)

if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ("temper-repo","closures","out"):
        parser.add_argument("--"+name,type=Path,required=True)
    main(parser.parse_args())
