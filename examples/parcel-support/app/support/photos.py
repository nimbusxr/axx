"""Photos: a customer whose parcel arrived damaged sends a photo of it from
the parcel's tracking page, which saves it in the uploads folder the support
desk shares with the tracking site (<parcel>/<photo>.png or .jpg). The
service keeps it with the parcel's ticket, and checks what it shows with an
image classifier (MobileNetV3, of ImageNet's classes), on the engine the
deployment chooses (SUPPORT_PHOTO_CHECK): torchvision's model, scripted with
TorchScript (the default), or converted for ncnn (with pnnx) or MNN (with
MNNConvert), which small ARM servers run it with, or exported for ExecuTorch
(delegated to XNNPACK); or Keras's, saved as a .keras file. The shop that sent the parcel needs a photo of it for its claim.
"""

import io
import logging
from collections.abc import Callable
from pathlib import Path

import numpy as np
from PIL import Image

from .folders import Watcher

log = logging.getLogger("support.photos")

# The classifier's labels, in the order of its scores.
LABELS = (Path(__file__).parent / "imagenet-labels.txt").read_text().splitlines()

# The labels of a parcel, and how sure of one the classifier must be.
PARCELS = {"carton", "packet", "envelope", "crate", "mailbag"}
CERTAINTY = 0.5

SIDE = 224
# ImageNet's channels' means and deviations, which torchvision's models take
# their pictures normalized by.
MEAN = np.array([0.485, 0.456, 0.406], dtype=np.float32)
STD = np.array([0.229, 0.224, 0.225], dtype=np.float32)


class PhotoCheck:
    """Names what a photo shows, and how likely."""

    ENGINES = ("torchscript", "ncnn", "mnn", "executorch", "keras")

    def __init__(self, model_dir: str, engine: str = "torchscript"):
        if engine not in self.ENGINES:
            raise ValueError(f"no photo check engine {engine!r}: one of {', '.join(self.ENGINES)}")
        self.engine = engine
        folder = Path(model_dir)
        if engine == "torchscript":
            import torch

            self.model = torch.jit.load(str(next(folder.glob("*.pt"))), map_location="cpu").eval()
        elif engine == "ncnn":
            import ncnn

            self.model = ncnn.Net()
            param = next(folder.glob("*.param"))
            self.model.load_param(str(param))
            self.model.load_model(str(param.with_suffix(".bin")))
        elif engine == "mnn":
            import MNN

            self.model = MNN.Interpreter(str(next(folder.glob("*.mnn"))))
            self.session = self.model.createSession()
        elif engine == "executorch":
            from executorch.runtime import Runtime

            program = Runtime.get().load_program(str(next(folder.glob("*.pte"))))
            self.model = program.load_method("forward")
        else:
            import keras

            self.model = keras.saving.load_model(str(next(folder.glob("*.keras"))))

    def check(self, photo: bytes) -> tuple[str, float]:
        image = Image.open(io.BytesIO(photo)).convert("RGB")
        if self.engine == "keras":
            # Keras's applications take the picture at their size, 0 to 255,
            # and give their classes' probabilities.
            x = np.asarray(image.resize((SIDE, SIDE), Image.BILINEAR), dtype=np.float32)[None]
            scores = np.asarray(self.model.predict(x, verbose=0))[0]
        else:
            scores = softmax(self.logits(torchvision_input(image)))
        best = int(scores.argmax())
        return LABELS[best], float(scores[best])

    def logits(self, x: np.ndarray) -> np.ndarray:
        """torchvision's model's logits of a picture, on the engine."""
        if self.engine == "torchscript":
            import torch

            with torch.no_grad():
                return self.model(torch.from_numpy(x[None]))[0].numpy()
        if self.engine == "executorch":
            import torch

            return self.model.execute([torch.from_numpy(x[None])])[0].numpy().reshape(-1)
        if self.engine == "ncnn":
            import ncnn

            with self.model.create_extractor() as ex:
                ex.input("in0", ncnn.Mat(x))
                _, out = ex.extract("out0")
                return np.array(out).reshape(-1)
        import MNN

        given = self.model.getSessionInput(self.session)
        given.copyFrom(MNN.Tensor((1, 3, SIDE, SIDE), MNN.Halide_Type_Float, x[None], MNN.Tensor_DimensionType_Caffe))
        self.model.runSession(self.session)
        return np.array(self.model.getSessionOutput(self.session).getNumpyData()).reshape(-1)


def torchvision_input(image: Image.Image) -> np.ndarray:
    """A picture as torchvision's evaluation transform makes it for the
    model: its shorter side resized to 256, its middle 224×224, normalized as
    ImageNet's pictures, channels first (in memory, as ncnn's Mat reads an
    array)."""
    scale = 256 / min(image.size)
    image = image.resize((round(image.width * scale), round(image.height * scale)), Image.BILINEAR)
    left, top = (image.width - SIDE) // 2, (image.height - SIDE) // 2
    image = image.crop((left, top, left + SIDE, top + SIDE))
    x = (np.asarray(image, dtype=np.float32) / 255 - MEAN) / STD
    return np.ascontiguousarray(x.transpose(2, 0, 1))


def softmax(logits: np.ndarray) -> np.ndarray:
    e = np.exp(logits - logits.max())
    return e / e.sum()


def shows_parcel(label: str, score: float) -> bool:
    return label in PARCELS and score >= CERTAINTY


class Uploads(Watcher):
    """Watches the uploads folder, and hands each photo to take, or a photo of
    a label (label.png) to label: its parcel and its bytes. Each says whether
    the photo belongs with a ticket: it is moved to kept/<parcel>, or set
    aside in unmatched/<parcel>."""

    name = "uploads"

    def __init__(
        self,
        folder: str,
        take: Callable[[str, bytes], bool],
        label: Callable[[str, bytes], bool] | None = None,
        every: float = 1.0,
    ):
        super().__init__(every)
        self.folder = Path(folder)
        self.take = take
        self.label = label

    def poll(self) -> None:
        photos = [p for p in self.folder.glob("PX-*/*") if p.suffix.lower() in (".png", ".jpg", ".jpeg")]
        for photo in self.settled(photos):
            parcel = photo.parent.name
            take = self.label if self.label and photo.stem == "label" else self.take
            try:
                kept = take(parcel, photo.read_bytes())
            except Exception:
                log.exception("photo %s failed; it stays in the uploads", photo)
                continue
            to = self.folder / ("kept" if kept else "unmatched") / parcel
            to.mkdir(parents=True, exist_ok=True)
            photo.replace(to / photo.name)
