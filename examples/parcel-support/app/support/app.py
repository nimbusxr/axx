"""The support desk's service: its API, and the mailbox it watches.

Its settings are environment variables:

  SUPPORT_DATABASE_URL  PostgreSQL's (postgresql://support:support@localhost:5432/support)
  SUPPORT_VOICEMAIL     the support line's mailbox folder, as the phone system keeps it
  SUPPORT_UPLOADS       the folder the tracking site saves customers' photos in
  SUPPORT_MODELS        the folder of the models' folders: transcriber, help-centre, reranker
                        (reranker.gguf), drafter, photo-check, voice (Piper's),
                        voicemail-screen (MediaPipe's audio classifier's)
  SUPPORT_TRANSCRIBER   the transcriber's engine: faster-whisper (the default), openai-whisper
                        or transformers, of a model of its format in the transcriber's folder
  SUPPORT_HELP_CENTRE   the help centre's engine: sentence-transformers (the default) or
                        llama.cpp, of a model of its format in the help centre's folder
  SUPPORT_DRAFTER       the drafter's engine: transformers (the default) or onnxruntime-genai,
                        of a model of its format in the drafter's folder
  SUPPORT_PHOTO_CHECK   the photo check's engine: torchscript (the default), ncnn, mnn,
                        executorch or keras, of a model of its format in the photo check's
                        folder
  SUPPORT_SPOOL         the phone system's spool folder (outgoing, tmp), which it takes
                        call-backs from; unset, voicemails get none
  SUPPORT_SOUNDS        the support line's sounds folder, which the phone system plays
                        call-backs' confirmations from
  SUPPORT_TRUNK         the SIP trunk the phone system calls customers back on
"""

import logging
import os
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Annotated

from fastapi import Body, FastAPI, HTTPException
from fastapi.concurrency import run_in_threadpool
from pydantic import BaseModel, Field

from .drafts import Drafter
from .callbacks import Callbacks, Speaker
from .helpcentre import Embedder, HelpCentre, Reranker
from .labels import LabelReader
from .photos import PhotoCheck, Uploads, shows_parcel
from .screen import Screen
from .tickets import Tickets
from .voicemail import Mailbox, Transcriber

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
log = logging.getLogger("support")


class Desk:
    """Opens tickets: with the help-centre article that answers the
    customer, and a draft reply of it, when one does."""

    def __init__(
        self,
        tickets,
        help_centre,
        drafter,
        transcriber=None,
        photo_check=None,
        callbacks=None,
        label_reader=None,
        screen=None,
    ):
        self.tickets = tickets
        self.help_centre = help_centre
        self.drafter = drafter
        self.transcriber = transcriber
        self.photo_check = photo_check
        self.callbacks = callbacks
        self.label_reader = label_reader
        self.screen = screen

    def open(self, channel: str, customer: str, message: str):
        article = self.help_centre.answer(message)
        draft = self.drafter.draft(article, message) if article else None
        ticket = self.tickets.open(channel, customer, message, article.slug if article else None, draft)
        log.info("ticket %s opened: %s from %s, article %s", ticket.id, channel, customer, ticket.article)
        return ticket

    def photo(self, parcel: str, photo: bytes) -> bool:
        """Keeps a photo of a parcel with its latest ticket, and what it
        shows: a photo of the parcel makes the ticket ready for the shop's
        claim. False when the parcel has no ticket."""
        ticket = self.tickets.of_parcel(parcel)
        if ticket is None:
            log.info("photo of %s set aside: no ticket about it", parcel)
            return False
        shows, score = self.photo_check.check(photo)
        status = "READY_FOR_CLAIM" if shows_parcel(shows, score) else ticket.status
        log.info("photo of %s (ticket %s) shows %s (%.2f): %s", parcel, ticket.id, shows, score, status)
        self.tickets.photo(ticket.id, photo, shows, score, status)
        return True

    def label(self, parcel: str, photo: bytes) -> bool:
        """Reads the reference on a photo of a parcel's label: the parcel's
        confirms its latest ticket's parcel. False when the parcel has no
        ticket, or the label is another parcel's."""
        ticket = self.tickets.of_parcel(parcel)
        if ticket is None:
            log.info("label of %s set aside: no ticket about it", parcel)
            return False
        shows = self.label_reader.read(photo)
        if shows != parcel:
            log.info("label of %s (ticket %s) set aside: it shows %s", parcel, ticket.id, shows)
            return False
        log.info("label of %s (ticket %s) confirms its parcel", parcel, ticket.id)
        self.tickets.label(ticket.id, shows)
        return True

    def voicemail(self, recording: Path, caller: str) -> None:
        if self.screen and not self.screen.speech(recording):
            log.info("voicemail %s from %s has no one speaking: no ticket", recording.name, caller)
            return
        transcript = self.transcriber.transcribe(recording)
        log.info("voicemail %s from %s: %s", recording.name, caller, transcript)
        ticket = self.open("voicemail", caller, transcript)
        if self.callbacks:
            self.callbacks.queue(recording.stem, caller, ticket.parcel)


class Message(BaseModel):
    customer: str = Field(description="The customer's e-mail address")
    text: str = Field(description="What they wrote")


EXAMPLES = {
    "question": {
        "summary": "A customer's question",
        "value": {"customer": "anna.weber@example.com", "text": "Can I still change the delivery address of PX-5102?"},
    }
}


def create(desk_factory=None) -> FastAPI:
    state = {}

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        if desk_factory:
            state["desk"] = desk_factory()
        else:
            models = Path(os.environ.get("SUPPORT_MODELS", "/models"))
            callbacks = None
            if spool := os.environ.get("SUPPORT_SPOOL"):
                callbacks = Callbacks(
                    Speaker(str(models / "voice")),
                    spool,
                    os.environ.get("SUPPORT_SOUNDS", "/var/lib/asterisk/sounds/support"),
                    os.environ.get("SUPPORT_TRUNK", "support-trunk"),
                )
            state["desk"] = Desk(
                Tickets(os.environ.get("SUPPORT_DATABASE_URL", "postgresql://support:support@localhost:5432/support")),
                HelpCentre(
                    Embedder(str(models / "help-centre"), os.environ.get("SUPPORT_HELP_CENTRE", "sentence-transformers")),
                    Reranker(str(models / "reranker" / "reranker.gguf")),
                ),
                Drafter(str(models / "drafter"), os.environ.get("SUPPORT_DRAFTER", "transformers")),
                Transcriber(str(models / "transcriber"), os.environ.get("SUPPORT_TRANSCRIBER", "faster-whisper")),
                PhotoCheck(str(models / "photo-check"), os.environ.get("SUPPORT_PHOTO_CHECK", "torchscript")),
                callbacks,
                LabelReader(),
                Screen(str(models / "voicemail-screen")),
            )
            log.info(
                "models loaded: transcriber on %s, help centre on %s, drafter on %s, photo check on %s",
                state["desk"].transcriber.engine,
                state["desk"].help_centre.model.engine,
                state["desk"].drafter.engine,
                state["desk"].photo_check.engine,
            )
        watchers = []
        if folder := os.environ.get("SUPPORT_VOICEMAIL"):
            watchers.append(Mailbox(folder, state["desk"].voicemail))
        if folder := os.environ.get("SUPPORT_UPLOADS"):
            watchers.append(Uploads(folder, state["desk"].photo, state["desk"].label))
        for w in watchers:
            w.start()
        yield
        for w in watchers:
            w.stop()
        if screen := getattr(state["desk"], "screen", None):
            screen.close()

    app = FastAPI(title="Parcel support", version="1.0.0", lifespan=lifespan)

    @app.get("/health")
    def health():
        return {"status": "UP"}

    @app.post("/api/messages", status_code=201, summary="A customer's message opens a ticket")
    async def message(m: Annotated[Message, Body(openapi_examples=EXAMPLES)]):
        ticket = await run_in_threadpool(state["desk"].open, "message", m.customer, m.text)
        return ticket.json()

    @app.get("/api/tickets/{ticket_id}", summary="A ticket")
    def ticket(ticket_id: str):
        t = state["desk"].tickets.get(ticket_id)
        if t is None:
            raise HTTPException(404, f"no ticket {ticket_id}")
        return t.json()

    return app


app = create()
