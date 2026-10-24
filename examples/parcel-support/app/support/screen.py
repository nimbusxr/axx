"""Screening: not every voicemail has a caller speaking in it. A call the
caller hung up on, or a line that stayed open, leaves a recording of its hum.
The service hears each recording with an audio classifier (YAMNet, of
AudioSet's sounds), which MediaPipe's audio classifier runs over each second
of it: a recording none of whose seconds is speech opens no ticket.
"""

import wave
from pathlib import Path

import numpy as np

# The sounds of someone speaking, as YAMNet names them.
SPEECH = {"Speech", "Child speech, kid speaking", "Conversation", "Narration, monologue"}


class Screen:
    """Hears whether a recording has someone speaking in it."""

    def __init__(self, model_dir: str):
        from mediapipe.tasks.python import audio
        from mediapipe.tasks.python.core.base_options import BaseOptions

        self.classifier = audio.AudioClassifier.create_from_options(
            audio.AudioClassifierOptions(
                base_options=BaseOptions(model_asset_path=str(next(Path(model_dir).glob("*.tflite")))),
                max_results=1,
            )
        )

    def close(self) -> None:
        self.classifier.close()

    def speech(self, recording: Path) -> bool:
        from mediapipe.tasks.python.components.containers import audio_data

        with wave.open(str(recording)) as w:
            rate, frames = w.getframerate(), w.readframes(w.getnframes())
        samples = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768
        seconds = self.classifier.classify(audio_data.AudioData.create_from_array(samples, rate))
        return any(
            s.classifications[0].categories and s.classifications[0].categories[0].category_name in SPEECH
            for s in seconds
        )
