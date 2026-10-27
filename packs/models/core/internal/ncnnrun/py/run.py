# Runs ncnn models (.param, its .bin beside it) with ncnn (its Python
# package's: the runtime apps link) for axx's tests of stubs: with ncnn's
# defaults, as apps run it (a CPU of ARM's stores and computes in half
# precision), or, with --full, in full precision. It reads a JSON list of
# runs on stdin, each a model's param file and its inputs ({type, dims,
# data}, by blob), and writes their outputs, by blob, as a JSON list on
# stdout.
import json
import os
import sys

import ncnn
import numpy as np


def main():
    full = "--full" in sys.argv[1:]
    runs = json.load(sys.stdin)
    stdout = os.fdopen(os.dup(1), "w")
    os.dup2(2, 1)
    nets = {}
    results = []
    for run in runs:
        net = nets.get(run["model"])
        if net is None:
            net = ncnn.Net()
            net.opt.use_vulkan_compute = False
            if full:
                for opt in ("use_fp16_packed", "use_fp16_storage", "use_fp16_arithmetic", "use_bf16_storage"):
                    setattr(net.opt, opt, False)
            if net.load_param(run["model"]) != 0 or net.load_model(os.path.splitext(run["model"])[0] + ".bin") != 0:
                raise SystemExit("ncnn cannot load " + run["model"])
            nets[run["model"]] = net
        ex = net.create_extractor()
        # A Mat of an array is of the array's memory: the arrays are kept
        # until the outputs are extracted.
        arrays = []
        for name, feed in run["feeds"].items():
            arrays.append(np.ascontiguousarray(np.array(feed["data"], dtype=np.float32).reshape(feed["dims"])))
            ex.input(name, ncnn.Mat(arrays[-1]))
        result = {}
        for name in net.output_names():
            r, m = ex.extract(name)
            if r != 0:
                raise SystemExit("ncnn cannot extract " + name)
            a = np.array(m).astype(np.float64)
            result[name] = {"type": "float32", "dims": list(a.shape), "data": a.flatten().tolist()}
        results.append(result)
    json.dump(results, stdout)
    stdout.flush()


if __name__ == "__main__":
    main()
