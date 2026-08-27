#!/usr/bin/env python3
"""Frozen Field Kit runner for the Qwen3.8 Dynamic short baseline."""

from __future__ import annotations

import argparse
import hashlib
import http.client
import json
import os
import re
import signal
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

SCHEMA = "field-kit-qwen38-dynamic-protocol/v1"
EXPECTED = "TEMPER-QWEN-OK"
SAMPLING = {"temperature": 0.0, "top_k": 1, "top_p": 1.0, "seed": 4242}


class Failure(RuntimeError):
    pass


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def write_report(path: Path, report: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    data = json.dumps(report, indent=2, sort_keys=True) + "\n"
    with tempfile.NamedTemporaryFile("w", dir=path.parent, delete=False) as handle:
        handle.write(data)
        staged = Path(handle.name)
    staged.replace(path)


def request(base: str, path: str, payload: dict | None = None, timeout: int = 900) -> bytes:
    body = None if payload is None else json.dumps(payload).encode()
    method = "GET" if body is None else "POST"
    item = urllib.request.Request(base + path, data=body, method=method, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(item, timeout=timeout) as response:
            return response.read()
    except urllib.error.HTTPError as error:
        detail = error.read().decode(errors="replace")[:500]
        raise Failure(f"HTTP {error.code} from {path}: {detail}") from error
    except urllib.error.URLError as error:
        raise Failure(f"request failed for {path}: {error.reason}") from error


def post(base: str, payload: dict) -> dict:
    return json.loads(request(base, "/v1/chat/completions", payload))


def payload(model: str, messages: list[dict], maximum: int) -> dict:
    return {"model": model, "messages": messages, "max_tokens": maximum, **SAMPLING}


def assistant(response: dict, label: str) -> tuple[dict, dict]:
    choices = response.get("choices")
    if not isinstance(choices, list) or len(choices) != 1:
        raise Failure(f"{label}: expected one choice")
    choice = choices[0]
    message = choice.get("message")
    if not isinstance(message, dict) or message.get("role") != "assistant":
        raise Failure(f"{label}: missing assistant message")
    return choice, message


def exact(response: dict, expected: str, label: str) -> dict:
    choice, message = assistant(response, label)
    content = message.get("content") or ""
    reasoning = message.get("reasoning_content") or ""
    if content.strip() != expected or reasoning or "<think>" in content:
        raise Failure(f"{label}: output was not exact non-reasoning content")
    return {
        "content_sha256": digest(content.encode()),
        "content_characters": len(content),
        "finish_reason": choice.get("finish_reason"),
        "usage": response.get("usage"),
        "timings": response.get("timings"),
    }


def stream_exact(base: str, model: str) -> dict:
    raw = request(base, "/v1/chat/completions", {
        **payload(model, [{"role": "user", "content": f"Reply with exactly {EXPECTED} and nothing else."}], 64),
        "stream": True, "stream_options": {"include_usage": True},
    })
    content: list[str] = []
    done = False
    chunks = 0
    for line in raw.decode(errors="replace").splitlines():
        if not line.startswith("data: "):
            continue
        value = line[6:]
        if value == "[DONE]":
            done = True
            continue
        chunk = json.loads(value)
        chunks += 1
        choices = chunk.get("choices") or []
        if choices and isinstance((choices[0].get("delta") or {}).get("content"), str):
            content.append(choices[0]["delta"]["content"])
        reasoning = (choices[0].get("delta") or {}).get("reasoning_content") if choices else None
        if reasoning:
            raise Failure("control/stream: unexpected reasoning content")
    value = "".join(content)
    if value.strip() != EXPECTED or not done:
        raise Failure("control/stream: output was not exact or stream did not finish")
    return {"content_sha256": digest(value.encode()), "content_characters": len(value), "chunks": chunks, "done": done}


def parse_tool_calls(response: dict, label: str) -> list[dict]:
    _, message = assistant(response, label)
    calls = message.get("tool_calls")
    if not isinstance(calls, list) or not calls:
        raise Failure(f"{label}: no tool calls")
    result = []
    for call in calls:
        function = call.get("function") or {}
        arguments = function.get("arguments")
        try:
            arguments = json.loads(arguments) if isinstance(arguments, str) else arguments
        except json.JSONDecodeError as error:
            raise Failure(f"{label}: invalid tool arguments") from error
        result.append({"id": str(call.get("id") or ""), "name": function.get("name"), "arguments": arguments})
    return result


def run_tools(base: str, model: str) -> dict:
    definitions = [
        {"type": "function", "function": {"name": "weather.lookup", "description": "Look up weather.", "parameters": {"type": "object", "properties": {"city": {"type": "string"}, "units": {"type": "string", "enum": ["celsius", "fahrenheit"]}}, "required": ["city", "units"], "additionalProperties": False}}},
        {"type": "function", "function": {"name": "clock.lookup", "description": "Look up local time.", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"], "additionalProperties": False}}},
    ]
    system = {"role": "system", "content": "Follow the request exactly. Emit only requested tool calls."}
    single_messages = [system, {"role": "user", "content": "Call weather.lookup exactly once for Stockholm using celsius."}]
    single_response = post(base, {**payload(model, single_messages, 512), "tools": definitions})
    single = parse_tool_calls(single_response, "tools/single")
    if len(single) != 1 or single[0]["name"] != "weather.lookup" or single[0]["arguments"] != {"city": "Stockholm", "units": "celsius"}:
        raise Failure("tools/single: call differed from expected")
    continuation = single_messages + [
        (single_response["choices"][0]["message"]),
        {"role": "tool", "tool_call_id": single[0]["id"], "content": "CITY_TEMP_MARKER_17C"},
        {"role": "user", "content": "Reply with exactly CITY_TEMP_MARKER_17C and nothing else."},
    ]
    continued = exact(post(base, payload(model, continuation, 128)), "CITY_TEMP_MARKER_17C", "tools/continuation")
    parallel_response = post(base, {
        **payload(model, [system, {"role": "user", "content": "Make exactly two parallel calls: weather.lookup for Stockholm in celsius and clock.lookup for Tokyo."}], 768),
        "tools": definitions, "parallel_tool_calls": True,
    })
    parallel = parse_tool_calls(parallel_response, "tools/parallel")
    observed = sorted((item["name"], json.dumps(item["arguments"], sort_keys=True)) for item in parallel)
    wanted = sorted([
        ("weather.lookup", json.dumps({"city": "Stockholm", "units": "celsius"}, sort_keys=True)),
        ("clock.lookup", json.dumps({"city": "Tokyo"}, sort_keys=True)),
    ])
    if observed != wanted:
        raise Failure("tools/parallel: calls differed from expected")
    return {"single": single, "continuation": continued, "parallel": parallel}


def run_history(base: str, model: str) -> dict:
    marker = "QWEN-HISTORY-9F3A"
    messages: list[dict] = []
    turns = []
    for turn in range(1, 5):
        expected = f"QWEN-TURN-{turn}-OK"
        prompt_text = f"Reply with exactly {expected}. Remember marker {marker} for later turns."
        if turn > 1:
            prompt_text = f"This is turn {turn}. Reply with exactly {expected}; the marker remains {marker}."
        messages.append({"role": "user", "content": prompt_text})
        response = post(base, payload(model, messages, 96))
        turns.append(exact(response, expected, f"history/{turn}"))
        messages.append(response["choices"][0]["message"])
    return {"turns": turns, "marker_sha256": digest(marker.encode())}


def cancel_and_recover(base: str, model: str) -> dict:
    parsed = urllib.parse.urlsplit(base)
    connection = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=60)
    body = json.dumps({
        **payload(model, [{"role": "user", "content": "Write the positive integers in order, one per line, for as long as possible."}], 4096),
        "stream": True,
    })
    path = (parsed.path.rstrip("/") if parsed.path else "") + "/v1/chat/completions"
    connection.request("POST", path, body=body, headers={"Content-Type": "application/json"})
    response = connection.getresponse()
    observed = response.read(256)
    connection.close()
    if not observed:
        raise Failure("cancel: server produced no streamed bytes")
    time.sleep(2)
    recovered = exact(post(base, payload(model, [{"role": "user", "content": f"Reply with exactly {EXPECTED} and nothing else."}], 64)), EXPECTED, "cancel/recovery")
    return {"cancelled_after_bytes": len(observed), "cancelled_prefix_sha256": digest(observed), "recovery": recovered}


def command_text(arguments: list[str]) -> str:
    return " ".join(arguments)


def resource_snapshot(swap_start: float) -> dict:
    swap_text = subprocess.check_output(["sysctl", "-n", "vm.swapusage"], text=True)
    match = re.search(r"used = ([0-9.]+)M", swap_text)
    if not match:
        raise Failure("could not parse vm.swapusage")
    swap = float(match.group(1))
    thermal = subprocess.check_output(["pmset", "-g", "therm"], text=True).strip()
    lower = thermal.lower()
    if "thermal warning level has been recorded" in lower and "no thermal warning" not in lower:
        raise Failure("thermal warning was recorded")
    if "performance warning level has been recorded" in lower and "no performance warning" not in lower:
        raise Failure("performance warning was recorded")
    if swap - swap_start >= 512:
        raise Failure(f"swap grew {swap - swap_start:.1f} MiB; 512 MiB stop reached")
    return {
        "swap_mib": swap,
        "swap_growth_mib": round(swap - swap_start, 3),
        "thermal_sha256": digest(thermal.encode()),
        "child_peak_memory": {"enforced": False, "reason": "portable router-root process telemetry is not exposed"},
    }


def wait_ready(base: str, process: subprocess.Popen, seconds: int = 300) -> None:
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise Failure(f"isolated router exited during startup with {process.returncode}")
        try:
            response = request(base, "/health", timeout=3).decode(errors="replace").strip()
            if response == "OK" or response.startswith("{"):
                return
        except Failure:
            pass
        time.sleep(1)
    raise Failure("isolated router did not become healthy within 300 seconds")


def stop(process: subprocess.Popen) -> None:
    if process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
        process.wait(timeout=20)
    except (ProcessLookupError, subprocess.TimeoutExpired):
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=10)


def interrupt(_signum: int, _frame: object) -> None:
    raise KeyboardInterrupt


def runtime_limit(_signum: int, _frame: object) -> None:
    raise Failure("live protocol exceeded its 20-minute runtime bound")


def main() -> int:
    signal.signal(signal.SIGTERM, interrupt)
    signal.signal(signal.SIGINT, interrupt)
    signal.signal(signal.SIGALRM, runtime_limit)
    parser = argparse.ArgumentParser()
    parser.add_argument("--temper", type=Path, required=True)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--software-lock", type=Path, required=True)
    parser.add_argument("--generation", required=True)
    parser.add_argument("--installation", required=True)
    parser.add_argument("--model", required=True)
    parser.add_argument("--listen", required=True)
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--log-dir", type=Path, required=True)
    args = parser.parse_args()
    base = "http://" + args.listen
    swap_text = subprocess.check_output(["sysctl", "-n", "vm.swapusage"], text=True)
    swap_match = re.search(r"used = ([0-9.]+)M", swap_text)
    if not swap_match:
        print("could not parse initial vm.swapusage", file=sys.stderr)
        return 2
    swap_start = float(swap_match.group(1))
    report = {"schema": SCHEMA, "status": "running", "model": args.model, "generation": args.generation, "started_at": time.time(), "swap_start_mib": swap_start, "checks": {}, "resources": []}
    write_report(args.report, report)
    args.log_dir.mkdir(parents=True, exist_ok=True)
    stdout_path = args.log_dir / "probe.stdout"
    stderr_path = args.log_dir / "probe.stderr"
    invocation = [str(args.temper), "probe", "serve", "--root", str(args.root), "--installation", args.installation, "--software-lock", str(args.software_lock), "--generation", args.generation, "--listen", args.listen]
    process = None
    try:
        signal.alarm(20 * 60)
        dry = subprocess.run(invocation + ["--dry-run"], capture_output=True, check=False)
        if dry.returncode != 0:
            raise Failure("Temper probe dry-run refused: " + dry.stderr.decode(errors="replace")[:500])
        with stdout_path.open("wb") as router_stdout, stderr_path.open("wb") as router_stderr:
            process = subprocess.Popen(invocation, stdout=router_stdout, stderr=router_stderr, start_new_session=True)
            wait_ready(base, process)
            models = json.loads(request(base, "/v1/models"))
            if args.model not in [item.get("id") for item in models.get("data", [])]:
                raise Failure("router model discovery did not contain the exact baseline layout")
            report["checks"]["models"] = {"count": len(models.get("data", [])), "response_sha256": digest(json.dumps(models, sort_keys=True).encode())}
            report["checks"]["control"] = exact(post(base, payload(args.model, [{"role": "user", "content": f"Reply with exactly {EXPECTED} and nothing else."}], 64)), EXPECTED, "control/nonstream")
            report["checks"]["stream"] = stream_exact(base, args.model)
            report["resources"].append(resource_snapshot(swap_start))
            report["checks"]["tools"] = run_tools(base, args.model)
            report["resources"].append(resource_snapshot(swap_start))
            report["checks"]["history"] = run_history(base, args.model)
            report["resources"].append(resource_snapshot(swap_start))
            report["checks"]["cancel"] = cancel_and_recover(base, args.model)
            report["resources"].append(resource_snapshot(swap_start))
            soak = []
            for index in range(3):
                started = time.monotonic()
                check = exact(post(base, payload(args.model, [{"role": "user", "content": f"Reply with exactly {EXPECTED} and nothing else."}], 64)), EXPECTED, f"soak/{index + 1}")
                check["wall_seconds"] = round(time.monotonic() - started, 6)
                soak.append(check)
            report["checks"]["soak"] = soak
            report["resources"].append(resource_snapshot(swap_start))
            report["status"] = "pass"
    except KeyboardInterrupt:
        report["status"] = "interrupted"
        report["error"] = "KeyboardInterrupt"
    except Exception as error:
        report["status"] = "fail"
        report["error"] = f"{type(error).__name__}: {error}"
    finally:
        signal.alarm(0)
        if process is not None:
            stop(process)
        report["finished_at"] = time.time()
        for label, path in (("probe_stdout", stdout_path), ("probe_stderr", stderr_path)):
            if path.exists():
                data = path.read_bytes()
                report[label] = {"path": path.name, "bytes": len(data), "sha256": digest(data)}
        write_report(args.report, report)
    print(json.dumps({"schema": SCHEMA, "status": report["status"], "report": str(args.report)}, sort_keys=True))
    return 0 if report["status"] == "pass" else (130 if report["status"] == "interrupted" else 3)


if __name__ == "__main__":
    raise SystemExit(main())
