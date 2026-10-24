# Runs MNN models (.mnn) with MNN (its Python package's Module API, the
# runtime apps link) for axx's tests of stubs: at MNN's normal precision, its
# default, as apps run it (a CPU of ARM's computes in half precision), or,
# with --high, at high precision. It reads a JSON list of
# runs on stdin, each a model's path and its inputs ({type, dims, data}, by
# name), and writes their outputs, by name, as a JSON list on stdout.
#
# With --llm, it runs MNN-LLM models with MNN's LLM engine (MNN.llm, as apps'
# Llm::createLLM): each job a model's config.json, settings over its config
# (its precision, backend, reuse_kv) and conversations, each of prompts; it
# answers each conversation's
# prompts in turn (the engine keeps the conversation), from a reset, and
# gives each job's answers (without the newline the engine ends each with),
# and its tokenizer's tokens of the texts given.
import json
import os
import sys

import MNN
import MNN.expr as F
import MNN.nn as nn
import numpy as np


def llm(jobs, stdout):
    import MNN.llm as mllm

    results = []
    for job in jobs:
        model = mllm.create(job["config"])
        if job.get("settings"):
            model.set_config(job["settings"])
        model.load()
        answers = []
        for conversation in job["conversations"]:
            model.reset()
            # response ends what the model says with its end_with, a newline.
            answers.append([model.response(prompt).removesuffix("\n") for prompt in conversation])
        tokens = [list(model.tokenizer_encode(text)) for text in job.get("encode") or []]
        results.append({"answers": answers, "tokens": tokens})
    json.dump(results, stdout)
    stdout.flush()


def main():
    if "--llm" in sys.argv[1:]:
        jobs = json.load(sys.stdin)
        stdout = os.fdopen(os.dup(1), "w")
        os.dup2(2, 1)
        llm(jobs, stdout)
        return
    precision = "high" if "--high" in sys.argv[1:] else "normal"
    runs = json.load(sys.stdin)
    # MNN prints on stdout: the results go on stdout as it was, and what MNN
    # prints on stderr.
    stdout = os.fdopen(os.dup(1), "w")
    os.dup2(2, 1)
    config = {"backend": "CPU", "precision": precision}
    rt = nn.create_runtime_manager((config,))
    modules = {}
    results = []
    for run in runs:
        module = modules.get(run["model"])
        if module is None:
            module = nn.load_module_from_file(run["model"], [], [], runtime_manager=rt)
            modules[run["model"]] = module
        info = module.get_info()
        inputs = []
        for name in info["inputNames"]:
            feed = run["feeds"][name]
            if feed["type"] in ("float32", "float16"):
                data, dtype = np.array(feed["data"], dtype=np.float32), F.float
            elif feed["type"] == "uint8":
                data, dtype = np.array(feed["data"], dtype=np.uint8), F.uint8
            else:
                data, dtype = np.array(feed["data"], dtype=np.int32), F.int
            inputs.append(F.const(data.flatten().tolist(), list(feed["dims"]), F.NCHW, dtype))
        outs = module.forward(inputs)
        result = {}
        for name, v in zip(info["outputNames"], outs):
            a = np.array(v.read()).astype(np.float64)
            result[name] = {"type": str(a.dtype), "dims": list(a.shape), "data": a.flatten().tolist()}
        results.append(result)
    json.dump(results, stdout)
    stdout.flush()


if __name__ == "__main__":
    main()
