# Runs models with MediaPipe Tasks (its Python package, the C++ tasks
# Android's, iOS's and the web's libraries run) for axx's tests of stubs. It
# reads a JSON list of jobs on stdin, each a task ("detect", "classify",
# "segment" or "embed", of pictures; "classifyAudio", of sounds;
# "classifyText" or "embedText", of texts), a model's path and pictures' or
# WAV files' paths or texts, and writes, as a JSON list on stdout, what
# MediaPipe made of each: its objects (label, score, and box, as shares of
# the picture's width and height), its classes (label, score), its masks
# (each pixel's label, and the last label's confidence), its embedding, or
# a sound's classes in each of its windows.
import json
import sys

import mediapipe as mp
from mediapipe.tasks import python
from mediapipe.tasks.python import vision

jobs = json.load(sys.stdin)
results = []
for job in jobs:
    base = python.BaseOptions(model_asset_path=job["model"])
    found = []
    if job["task"] == "detect":
        task = vision.ObjectDetector.create_from_options(vision.ObjectDetectorOptions(
            base_options=base, score_threshold=job.get("scoreThreshold", 0.3), max_results=job.get("maxResults", 10)))
        for path in job["images"]:
            img = mp.Image.create_from_file(path)
            objects = []
            for d in task.detect(img).detections:
                b = d.bounding_box
                objects.append({"label": d.categories[0].category_name, "score": d.categories[0].score,
                                "box": [b.origin_x / img.width, b.origin_y / img.height, b.width / img.width, b.height / img.height]})
            found.append(objects)
    elif job["task"] == "classify":
        task = vision.ImageClassifier.create_from_options(vision.ImageClassifierOptions(
            base_options=base, max_results=job.get("maxResults", 3)))
        for path in job["images"]:
            img = mp.Image.create_from_file(path)
            classes = task.classify(img).classifications[0].categories
            found.append([{"label": c.category_name, "score": c.score} for c in classes])
    elif job["task"] == "segment":
        task = vision.ImageSegmenter.create_from_options(vision.ImageSegmenterOptions(
            base_options=base, output_category_mask=True, output_confidence_masks=True))
        for path in job["images"]:
            img = mp.Image.create_from_file(path)
            r = task.segment(img)
            mask = {"width": img.width, "height": img.height}
            if r.category_mask is not None:
                mask["category"] = r.category_mask.numpy_view().astype("int32").flatten().tolist()
            if r.confidence_masks:
                mask["confidence"] = [round(float(v), 4) for v in r.confidence_masks[-1].numpy_view().flatten()]
            found.append(mask)
    elif job["task"] == "embed":
        task = vision.ImageEmbedder.create_from_options(vision.ImageEmbedderOptions(base_options=base))
        for path in job["images"]:
            img = mp.Image.create_from_file(path)
            found.append([float(v) for v in task.embed(img).embeddings[0].embedding])
    elif job["task"] == "classifyAudio":
        import wave

        import numpy as np
        from mediapipe.tasks.python import audio
        from mediapipe.tasks.python.components.containers import audio_data

        task = audio.AudioClassifier.create_from_options(audio.AudioClassifierOptions(base_options=base, max_results=3))
        for path in job["sounds"]:
            with wave.open(path) as w:
                rate, frames = w.getframerate(), w.readframes(w.getnframes())
            samples = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768
            windows = task.classify(audio_data.AudioData.create_from_array(samples, rate))
            found.append([[{"label": c.category_name, "score": c.score} for c in w.classifications[0].categories] for w in windows])
    elif job["task"] == "classifyText":
        from mediapipe.tasks.python import text

        task = text.TextClassifier.create_from_options(text.TextClassifierOptions(base_options=base, max_results=2))
        for t in job["texts"]:
            found.append([{"label": c.category_name, "score": c.score} for c in task.classify(t).classifications[0].categories])
    elif job["task"] == "embedText":
        from mediapipe.tasks.python import text

        task = text.TextEmbedder.create_from_options(text.TextEmbedderOptions(base_options=base))
        for t in job["texts"]:
            found.append([float(v) for v in task.embed(t).embeddings[0].embedding])
    else:
        raise SystemExit("no task " + job["task"])
    task.close()
    results.append(found)
json.dump(results, sys.stdout)
