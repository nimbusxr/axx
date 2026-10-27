# Runs speech recognizers with faster-whisper (CTranslate2's Whisper: a
# folder of model.bin, its config.json, vocabulary and tokenizer.json) for
# axx's tests of stubs. It reads a JSON list of jobs on stdin, each a
# model's folder, sounds' samples (16 kHz) and a language (detected, when
# none), and writes each job's transcripts (their segments' texts) and
# their segments (start, end, text: of timestamps), as a JSON list on
# stdout. It runs CTranslate2 of the job's compute type: float32 (faster-
# whisper's default of a model of float32 on a CPU) when it gives none.
import json
import os
import sys

import numpy as np
from faster_whisper import WhisperModel


def main():
    jobs = json.load(sys.stdin)
    stdout = os.fdopen(os.dup(1), "w")
    os.dup2(2, 1)
    results = []
    for job in jobs:
        model = WhisperModel(job["model"], device="cpu", compute_type=job.get("computeType") or "float32")
        texts, segments = [], []
        for samples in job["transcribe"]:
            audio = np.array(samples, dtype=np.float32)
            got, _ = model.transcribe(audio, language=job.get("language") or None, without_timestamps=True, vad_filter=False)
            texts.append("".join(s.text for s in got))
            got, _ = model.transcribe(audio, language=job.get("language") or None, vad_filter=False)
            segments.append(json.dumps([{"start": s.start, "end": s.end, "text": s.text} for s in got]))
        results.append({"answers": texts, "segments": segments})
    json.dump(results, stdout)
    stdout.flush()


if __name__ == "__main__":
    main()
