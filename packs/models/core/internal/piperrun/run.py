import array
import base64
import json
import sys
from pathlib import Path

from piper import PiperVoice

jobs = json.load(sys.stdin)
answers = []
for i, job in enumerate(jobs):
    model = Path(f"/tmp/axx-voice-{i}.onnx")
    model.write_bytes(base64.b64decode(job["model"]))
    Path(str(model) + ".json").write_bytes(base64.b64decode(job["config"]))
    voice = PiperVoice.load(model)
    pcm = b"".join(voice.synthesize_stream_raw(job["text"]))
    samples = array.array("h")
    samples.frombytes(pcm)
    if sys.byteorder != "little":
        samples.byteswap()
    answers.append(samples.tolist())
json.dump(answers, sys.stdout)
