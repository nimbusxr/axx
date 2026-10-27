# Runs TorchScript modules with PyTorch (torch.jit.load: the loader Python
# services, LibTorch and Triton's PyTorch backend share), and PyTorch Mobile
# modules with its lite interpreter, for axx's tests of stubs. It reads a JSON list of runs on stdin, each a module's path and
# its inputs ({type, dims, data}, by its forward's parameters' names), and
# writes their outputs (output0, output1...: a tuple's, in order) as a JSON
# list on stdout.
import json
import sys

import torch

TYPES = {"float32": torch.float32, "float16": torch.float16, "int64": torch.int64, "int32": torch.int32, "uint8": torch.uint8, "int8": torch.int8, "bool": torch.bool}


def main():
    modules = {}
    results = []
    for run in json.load(sys.stdin):
        if run["model"] not in modules:
            # A PyTorch Mobile module (.ptl) runs in its lite interpreter, of
            # its bytecode; its forward's parameters are its code's.
            full = torch.jit.load(run["model"])
            names = [a.name for a in full.forward.schema.arguments[1:]]
            if run["model"].endswith(".ptl"):
                from torch.jit.mobile import _load_for_lite_interpreter

                module = _load_for_lite_interpreter(run["model"])
            else:
                module = full
                module.eval()
            modules[run["model"]] = (module, names)
        module, names = modules[run["model"]]
        inputs = [torch.tensor(run["feeds"][n]["data"], dtype=torch.float64).reshape(run["feeds"][n]["dims"]).to(TYPES[run["feeds"][n]["type"]]) for n in names]
        with torch.no_grad():
            outs = module(*inputs)
        if not isinstance(outs, tuple):
            outs = (outs,)
        results.append({f"output{i}": {"type": str(v.dtype), "dims": list(v.shape), "data": v.to(torch.float64).flatten().tolist()} for i, v in enumerate(outs)})
    json.dump(results, sys.stdout)


if __name__ == "__main__":
    main()
