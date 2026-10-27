# Runs TFLite models with LiteRT's interpreter (its Python package, the C++
# interpreter Android's and iOS's apps run, which has every tensor type:
# int8 too) for axx's tests of stubs. It reads a JSON list of runs on stdin,
# each a model's path and its inputs ({type, dims, data}, by name), and
# writes their outputs, by name, as a JSON list on stdout.
import json
import sys

import numpy as np
from ai_edge_litert.interpreter import Interpreter

runs = json.load(sys.stdin)
results = []
for run in runs:
    it = Interpreter(model_path=run["model"])
    for d in it.get_input_details():
        feed = run["feeds"][d["name"]]
        it.resize_tensor_input(d["index"], feed["dims"])
    it.allocate_tensors()
    for d in it.get_input_details():
        feed = run["feeds"][d["name"]]
        it.set_tensor(d["index"], np.array(feed["data"]).astype(d["dtype"]).reshape(feed["dims"]))
    it.invoke()
    out = {}
    for d in it.get_output_details():
        v = it.get_tensor(d["index"])
        out[d["name"]] = {"type": str(v.dtype), "dims": list(v.shape), "data": [float(x) for x in v.flatten()]}
    results.append(out)
json.dump(results, sys.stdout)
