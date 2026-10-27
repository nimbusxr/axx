# Runs OpenVINO models (their IR: model.xml, model.bin beside it) with
# OpenVINO's runtime, on the CPU in full precision (on ARM, it computes in
# half precision unless told not to), for axx's tests of stubs. It reads a JSON
# list of runs on stdin, each a model's path and its inputs ({type, dims,
# data}, by name), and writes their outputs, by name, as a JSON list on
# stdout.
import json
import sys

import numpy as np
import openvino as ov

def main():
    types = {"float32": np.float32, "float16": np.float16, "int64": np.int64, "int32": np.int32, "uint8": np.uint8, "int8": np.int8, "bool": np.bool_}
    core = ov.Core()
    compiled = {}
    results = []
    for run in json.load(sys.stdin):
        model = compiled.get(run["model"])
        if model is None:
            model = core.compile_model(core.read_model(run["model"]), "CPU", {"INFERENCE_PRECISION_HINT": "f32"})
            compiled[run["model"]] = model
        feeds = {}
        for port in model.inputs:
            feed = run["feeds"][port.any_name]
            feeds[port.any_name] = np.array(feed["data"]).astype(types[feed["type"]]).reshape(feed["dims"])
        outs = model(feeds)
        result = {}
        for port in model.outputs:
            v = outs[port]
            result[port.any_name] = {"type": str(v.dtype), "dims": list(v.shape), "data": [float(x) for x in np.asarray(v).flatten()]}
        results.append(result)
    json.dump(results, sys.stdout)


# OpenVINO starts processes of its own with multiprocessing, which runs this
# script again in them: only its own run reads the runs.
if __name__ == "__main__":
    main()
