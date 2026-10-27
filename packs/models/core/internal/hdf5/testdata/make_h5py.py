# Writes h5py.h5, a file of h5py's defaults (HDF5's earliest structures),
# for hdf5_test.go: python make_h5py.py, with h5py.
import h5py
import numpy as np

with h5py.File("h5py.h5", "w") as f:
    f.attrs["keras_version"] = "2.15.0"
    f.attrs["model_config"] = b'{"class_name": "Sequential"}'
    f.attrs["layer_names"] = np.array([b"dense", b"dense_1"])
    w = f.create_group("model_weights")
    for i in range(12):
        w.create_group("layer_%02d" % i)
    w.create_group("dense").create_dataset("kernel:0", data=np.array([[0.5, -1], [2, 3]], dtype=np.float32))
