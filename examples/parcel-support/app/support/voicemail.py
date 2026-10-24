"""Voicemails: customers who call the support line outside its hours leave a
message. The phone system (Asterisk) saves each in the support mailbox's
folder (INBOX/msgNNNN.wav, and msgNNNN.txt of its details: who called), and
the service takes it from there: it transcribes the recording with a speech
recognizer (Whisper base.en, which faster-whisper runs in the service, or
OpenAI's whisper package or transformers; or NVIDIA's Parakeet, which
parakeet.cpp's command runs; as the deployment chooses), opens
a ticket of the transcript, and moves the message to the mailbox's Old
folder, as listening to it on a phone does.
"""

import logging
import re
import subprocess
from collections.abc import Callable
from pathlib import Path

from .folders import Watcher

log = logging.getLogger("support.voicemail")

# The caller in a message's details: callerid="Anna Weber" <+493415550101>.
CALLER = re.compile(r'^callerid=(?:"[^"]*"\s*)?<?([^>\s]+)>?\s*$', re.MULTILINE)


class Transcriber:
    """Transcribes recordings on the engine the deployment chooses
    (SUPPORT_TRANSCRIBER), from the model's folder in that engine's format:
    Whisper's, for faster-whisper (CTranslate2's model.bin, the default),
    OpenAI's whisper package (its .pt checkpoint) or transformers (a Hugging
    Face model's folder), which read recordings with ffmpeg; or Parakeet's
    GGUF, for parakeet.cpp's command (its CTC decoder: Parakeet TDT-CTC's,
    which writes punctuation as Whisper does), which reads them itself."""

    ENGINES = ("faster-whisper", "openai-whisper", "transformers", "parakeet.cpp")

    def __init__(self, model_dir: str, engine: str = "faster-whisper"):
        if engine not in self.ENGINES:
            raise ValueError(f"no transcriber engine {engine!r}: one of {', '.join(self.ENGINES)}")
        self.engine = engine
        if engine == "faster-whisper":
            from faster_whisper import WhisperModel

            # On the CPU, of 8-bit integers, as faster-whisper's README runs it.
            self.model = WhisperModel(model_dir, device="cpu", compute_type="int8")
        elif engine == "openai-whisper":
            import whisper

            self.model = whisper.load_model(str(next(Path(model_dir).glob("*.pt"))), device="cpu")
        elif engine == "parakeet.cpp":
            self.model = str(next(Path(model_dir).glob("*.gguf")))
        else:
            from transformers import pipeline

            self.model = pipeline("automatic-speech-recognition", model=model_dir, device="cpu")

    def transcribe(self, recording: Path) -> str:
        if self.engine == "faster-whisper":
            segments, _ = self.model.transcribe(str(recording))
            return "".join(s.text for s in segments).strip()
        if self.engine == "openai-whisper":
            return self.model.transcribe(str(recording), fp16=False)["text"].strip()
        if self.engine == "parakeet.cpp":
            out = subprocess.run(
                ["parakeet-cli", "transcribe", "--model", self.model, "--input", str(recording), "--decoder", "ctc"],
                capture_output=True, text=True, check=True,
            )
            return out.stdout.strip()
        return self.model(str(recording))["text"].strip()


def caller(details: str) -> str:
    """The caller's number, from a message's details: unknown when it has none."""
    m = CALLER.search(details)
    return m.group(1) if m else "unknown"


class Mailbox(Watcher):
    """Watches a mailbox's INBOX, and hands each message to take: its
    recording and its caller. A recording is taken once it stops growing (the
    phone system writes it as the caller speaks)."""

    name = "mailbox"

    def __init__(self, folder: str, take: Callable[[Path, str], None], every: float = 1.0):
        super().__init__(every)
        self.inbox = Path(folder) / "INBOX"
        self.old = Path(folder) / "Old"
        self.take = take

    def poll(self) -> None:
        if not self.inbox.is_dir():
            return
        for recording in self.settled(self.inbox.glob("msg*.wav")):
            details = recording.with_suffix(".txt")
            who = caller(details.read_text(errors="replace")) if details.exists() else "unknown"
            try:
                self.take(recording, who)
            except Exception:
                log.exception("voicemail %s failed; it stays in the inbox", recording.name)
                continue
            self.old.mkdir(exist_ok=True)
            for f in (recording, details):
                if f.exists():
                    f.replace(self.old / f.name)
