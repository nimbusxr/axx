"""Labels: a customer who writes about a damaged parcel photographs its label
from the parcel's tracking page, which crops the photo to the label's
reference line and saves it in the uploads folder (<parcel>/label.png). The
service reads the reference with the system's Tesseract (Debian's
tesseract-ocr, of its English model), the crop as the one line it is (page
segmentation mode 13, a raw line): a label of the parcel confirms the
ticket's parcel; a label of another parcel is set aside.
"""

import subprocess
import tempfile

from .tickets import PARCEL


class LabelReader:
    """Reads the parcel's reference on a label's reference line, with
    Tesseract's command-line program and its language's model."""

    def __init__(self, program: str = "tesseract", language: str = "eng"):
        self.program = program
        self.language = language

    def read(self, photo: bytes) -> str | None:
        with tempfile.NamedTemporaryFile(suffix=".png") as f:
            f.write(photo)
            f.flush()
            text = subprocess.run(
                [self.program, f.name, "stdout", "--psm", "13", "-l", self.language, "--dpi", "300"],
                capture_output=True,
                check=True,
                text=True,
            ).stdout
        m = PARCEL.search(text)
        return m.group(0) if m else None
