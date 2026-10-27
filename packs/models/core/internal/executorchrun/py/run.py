# Runs ExecuTorch programs (.pte) with ExecuTorch's runtime (its Python
# package's: the portable and optimized kernels apps link) for axx's tests
# of stubs. It reads a JSON object on stdin, and writes a JSON list of its
# results on stdout:
#
# - {"runs": [...]}: each a program's path and its inputs ({type, dims,
#   data}, by name: input0, input1..., in order); its result, its outputs
#   (output0, output1...). A program's method other than forward follows
#   its path after a #.
# - {"chats": [...]}: each a language model's program, the folder of its
#   tokenizer (tokenizer.json, and tokenizer_config.json's chat template,
#   which the app renders as React Native ExecuTorch does), a system prompt
#   and chats, which ExecuTorch's text LLM runner answers (the one apps
#   link), each in a conversation of its own or, continued, one after
#   another in one; its result, the answers.
# - {"transcribe": [...]}: each a speech recognizer's program (encode and
#   decode, as React Native ExecuTorch exports Whisper) and sounds, which it
#   transcribes as React Native ExecuTorch does: encode of the sound; decode
#   of each token the app forces (the prompt), a call each, then of each it
#   generates (its logits' argmax), until the end; its result, each sound's
#   tokens.
import json
import os
import sys

import jinja2
import numpy as np
import torch
from executorch.extension.llm.runner import GenerationConfig, TextLLMRunner
from executorch.runtime import Runtime


def run(runs):
    types = {"float32": torch.float32, "float16": torch.float16, "int64": torch.int64, "int32": torch.int32, "uint8": torch.uint8, "int8": torch.int8, "bool": torch.bool}
    runtime = Runtime.get()
    methods = {}
    results = []
    for run in runs:
        method = methods.get(run["model"])
        if method is None:
            path, _, name = run["model"].partition("#")
            method = runtime.load_program(path).load_method(name or "forward")
            methods[run["model"]] = method
        inputs = []
        for i in range(len(run["feeds"])):
            feed = run["feeds"][f"input{i}"]
            inputs.append(torch.from_numpy(np.array(feed["data"]).reshape(feed["dims"])).to(types[feed["type"]]))
        outs = method.execute(inputs)
        result = {}
        for i, v in enumerate(outs):
            v = v.to(torch.float64) if v.dtype != torch.bool else v.to(torch.float64)
            result[f"output{i}"] = {"type": str(v.dtype), "dims": list(v.shape), "data": v.flatten().tolist()}
        results.append(result)
    return results


def chat(jobs):
    results = []
    for job in jobs:
        with open(f"{job['tokenizer']}/tokenizer_config.json") as f:
            config = json.load(f)
        template = jinja2.Environment().from_string(config["chat_template"])
        runner = TextLLMRunner(job["model"], f"{job['tokenizer']}/tokenizer.json")
        answers = []
        for c in job["chats"]:
            messages = [{"role": "system", "content": job["system"]}, {"role": "user", "content": c["prompt"]}]
            prompt = template.render(messages=messages, add_generation_prompt=True, bos_token=config.get("bos_token"), eos_token=config.get("eos_token"))
            pieces = []
            runner.generate(prompt, GenerationConfig(echo=False, temperature=c["temperature"], max_new_tokens=c.get("maxTokens", 64)), pieces.append)
            answers.append("".join(pieces).split(config["eos_token"])[0])
            if not job.get("continued"):
                runner.reset()
        results.append({"answers": answers})
    return results


def transcribe(jobs):
    runtime = Runtime.get()
    results = []
    for job in jobs:
        program = runtime.load_program(job["model"])
        encode, decode = program.load_method("encode"), program.load_method("decode")
        tokens = []
        for sound in job["sounds"]:
            hidden = encode.execute([torch.tensor(sound, dtype=torch.float32)])[0]

            def step(token, position):
                logits = decode.execute([torch.tensor([[token]], dtype=torch.int64), torch.tensor([position], dtype=torch.int64), hidden])[0]
                return int(torch.argmax(logits.reshape(-1)))

            for position, token in enumerate(job["prompt"]):
                following = step(token, position)
            position = len(job["prompt"])
            generated = []
            while len(generated) < job["maxTokens"] and following != job["end"]:
                generated.append(following)
                following = step(following, position)
                position += 1
            tokens.append(generated)
        results.append({"tokens": tokens})
    return results


def main():
    job = json.load(sys.stdin)
    # The runner prints what it generates, and its stats, on stdout: the
    # results go on stdout as it was, and what the runner prints on stderr.
    results = os.fdopen(os.dup(1), "w")
    os.dup2(2, 1)
    if "runs" in job:
        out = run(job["runs"])
    elif "chats" in job:
        out = chat(job["chats"])
    else:
        out = transcribe(job["transcribe"])
    json.dump(out, results)
    results.flush()


if __name__ == "__main__":
    main()
