# Runs TensorFlow SavedModels with TensorFlow (its Python package's: the
# runtime TF Serving and Python services load them with) for axx's tests
# of stubs. It reads a JSON list of runs on stdin, each a model's
# saved_model.pb (or a file of one, of any name) and its inputs ({type,
# dims, data}, by its signature's keys), and writes their outputs, by
# their keys, as a JSON list on stdout. A run calls the model by its
# signature (serving_default, unless it names another), as tf.saved_model.load
# gives it; or, of a "session", in a TensorFlow 1 session by its tensors'
# names, as apps of TensorFlow 1 call it; or, of "keras" ("keras", or
# "tf_keras": Keras 2), a Keras model (.keras, .h5) by its inputs' names, as
# load_model gives it, its outputs by their names.
import json
import os
import shutil
import sys
import tempfile

import numpy as np
import tensorflow as tf

TYPES = {"float32": np.float32, "float16": np.float16, "int64": np.int64, "int32": np.int32, "uint8": np.uint8, "int8": np.int8, "bool": np.bool_}


def folder(path):
    if os.path.basename(path) == "saved_model.pb":
        return os.path.dirname(path)
    d = tempfile.mkdtemp()
    shutil.copy(path, os.path.join(d, "saved_model.pb"))
    return d


def result(outs):
    return {k: {"type": str(v.dtype), "dims": list(v.shape), "data": np.asarray(v).astype(np.float64).flatten().tolist()} for k, v in outs.items()}


def main():
    runs = json.load(sys.stdin)
    loaded = {}
    results = []
    for run in runs:
        feeds = {k: np.array(v["data"]).reshape(v["dims"]).astype(TYPES[v["type"]]) for k, v in run["feeds"].items()}
        if "session" in run:
            # The tensors' names: of the feeds' keys, and of the outputs'.
            names = run["session"]
            with tf.compat.v1.Session(graph=tf.Graph()) as sess:
                tf.compat.v1.saved_model.loader.load(sess, ["serve"], folder(run["model"]))
                outs = sess.run({k: names["outputs"][k] for k in names["outputs"]}, {names["inputs"][k]: v for k, v in feeds.items()})
            results.append(result(outs))
            continue
        if "keras" in run:
            key = (run["keras"], run["model"])
            model = loaded.get(key)
            if model is None:
                if run["keras"] == "tf_keras":
                    import tf_keras as library
                else:
                    import keras as library
                model = library.models.load_model(run["model"], compile=False)
                loaded[key] = model
            outs = model({k: tf.constant(v) for k, v in feeds.items()}, training=False)
            if not isinstance(outs, (list, tuple)):
                outs = [outs]
            results.append(result({k: np.asarray(v) for k, v in zip(model.output_names, outs)}))
            continue
        model = loaded.get(run["model"])
        if model is None:
            model = tf.saved_model.load(folder(run["model"]))
            loaded[run["model"]] = model
        fn = model.signatures[run.get("signature", "serving_default")]
        outs = fn(**{k: tf.constant(v) for k, v in feeds.items()})
        results.append(result({k: v.numpy() for k, v in outs.items()}))
    json.dump(results, sys.stdout)


if __name__ == "__main__":
    main()
