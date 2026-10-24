"""Call-backs: a customer who leaves a voicemail after hours gets a call back
from the support line, which tells them their message is in. The phone
system (Asterisk) makes the call and plays a confirmation that the service
speaks with a voice of Piper's (its command-line program): into the support
line's sounds, at the voice's 16 kHz (a .wav16 file, as Asterisk plays a
16 kHz WAV). The service queues the call as Asterisk takes calls: a call file
in its spool's outgoing folder, written in its tmp folder and moved in whole.
"""

import logging
import os
import subprocess
from pathlib import Path

log = logging.getLogger("support.callbacks")


class Speaker:
    """Speaks a text with a Piper voice (its .onnx model, the .onnx.json
    config beside it), with Piper's command-line program."""

    def __init__(self, voice_dir: str, program: str = "piper"):
        self.model = str(next(Path(voice_dir).glob("*.onnx")))
        self.program = program

    def speak(self, text: str, wav: Path) -> None:
        subprocess.run(
            [self.program, "--model", self.model, "--output_file", str(wav)],
            input=text.encode(),
            capture_output=True,
            check=True,
        )


def confirmation(parcel: str | None) -> str:
    """What the call-back says: the parcel the message is about, when it
    names one."""
    if parcel:
        return f"Thank you for your message about parcel {parcel}. We will call you back tomorrow."
    return "Thank you for your message. We will call you back tomorrow."


class Callbacks:
    """Queues the phone system's call-backs: spool is Asterisk's spool folder
    (its outgoing and tmp folders), sounds the support line's sounds, and
    trunk the SIP trunk the calls go out on."""

    def __init__(self, speaker: Speaker, spool: str, sounds: str, trunk: str):
        self.speaker = speaker
        self.spool = Path(spool)
        self.sounds = Path(sounds)
        self.trunk = trunk

    def queue(self, message: str, caller: str, parcel: str | None) -> None:
        """Calls back the caller of a voicemail (msg0001), with its
        confirmation."""
        if not caller.startswith("+"):
            log.info("no call-back for %s: the caller left no number", message)
            return
        self.sounds.mkdir(parents=True, exist_ok=True)
        self.speaker.speak(confirmation(parcel), self.sounds / f"{message}.wav16")
        call = (
            f"Channel: PJSIP/{caller}@{self.trunk}\n"
            'CallerID: "Parcel support"\n'
            "MaxRetries: 2\n"
            "RetryTime: 600\n"
            "Application: Playback\n"
            f"Data: {self.sounds.name}/{message}\n"
        )
        tmp, outgoing = self.spool / "tmp", self.spool / "outgoing"
        tmp.mkdir(parents=True, exist_ok=True)
        outgoing.mkdir(parents=True, exist_ok=True)
        (tmp / f"{message}.call").write_text(call)
        os.replace(tmp / f"{message}.call", outgoing / f"{message}.call")
        log.info("call-back of %s queued: %s", message, caller)
