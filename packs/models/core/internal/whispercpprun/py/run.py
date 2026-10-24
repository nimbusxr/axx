# Runs speech recognizers with whisper.cpp (pywhispercpp, its Python
# binding: whisper.cpp's library, built as its releases build it) for axx's
# tests of stubs. It reads a JSON list of jobs on stdin, each a ggml model
# file, sounds' samples (16 kHz), a language (detected, when none) and
# whether to run on the machine's GPU (Metal, on a Mac: whisper.cpp's
# default there), and writes each job's transcripts (their segments' texts)
# and their segments (start, end, text: of timestamps), as a JSON list on
# stdout.
import json
import os
import sys

import _pywhispercpp as pw
import numpy as np
from pywhispercpp.model import Model


def segments(model):
    # The segments as whisper.cpp writes them (the binding's Segment strips
    # their text).
    ctx = model._ctx
    out = []
    for i in range(pw.whisper_full_n_segments(ctx)):
        text = pw.whisper_full_get_segment_text(ctx, i).decode("utf-8", errors="replace")
        out.append({"start": pw.whisper_full_get_segment_t0(ctx, i) / 100, "end": pw.whisper_full_get_segment_t1(ctx, i) / 100, "text": text})
    return out


def main():
    jobs = json.load(sys.stdin)
    stdout = os.fdopen(os.dup(1), "w")
    os.dup2(2, 1)
    results = []
    for job in jobs:
        model = Model(
            job["model"],
            redirect_whispercpp_logs_to=None,
            context_params={"use_gpu": job.get("gpu", False)},
            print_progress=False,
            print_realtime=False,
            language=job.get("language") or "",
        )
        texts, spans = [], []
        for samples in job["transcribe"]:
            audio = np.array(samples, dtype=np.float32)
            model.transcribe(audio, no_timestamps=True)
            texts.append("".join(s["text"] for s in segments(model)))
            model.transcribe(audio, no_timestamps=False)
            spans.append(json.dumps(segments(model)))
        results.append({"answers": texts, "segments": spans})
    json.dump(results, stdout)
    stdout.flush()


if __name__ == "__main__":
    main()
