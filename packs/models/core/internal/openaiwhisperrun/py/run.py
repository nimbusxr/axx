# Runs speech recognizers with OpenAI's whisper package (openai-whisper:
# whisper.load_model of a checkpoint's path, and its transcribe) for axx's
# tests of stubs. It reads a JSON list of jobs on stdin, each a checkpoint,
# sounds' samples (16 kHz) and a language (detected, when none), and writes
# each job's transcripts and their segments (start, end, text: of
# timestamps), as a JSON list on stdout. It runs on the CPU, in float32, as
# whisper does there.
import json
import os
import sys

import numpy as np
import whisper


def main():
    jobs = json.load(sys.stdin)
    stdout = os.fdopen(os.dup(1), "w")
    os.dup2(2, 1)
    results = []
    for job in jobs:
        model = whisper.load_model(job["model"], device="cpu")
        texts, segments = [], []
        for samples in job["transcribe"]:
            audio = np.array(samples, dtype=np.float32)
            options = {"language": job.get("language") or None, "fp16": False}
            texts.append(model.transcribe(audio, without_timestamps=True, **options)["text"])
            got = model.transcribe(audio, **options)["segments"]
            segments.append(json.dumps([{"start": s["start"], "end": s["end"], "text": s["text"]} for s in got]))
        results.append({"answers": texts, "segments": segments})
    json.dump(results, stdout)
    stdout.flush()


if __name__ == "__main__":
    main()
