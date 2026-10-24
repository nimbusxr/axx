from datetime import UTC, datetime
from pathlib import Path

from fastapi.testclient import TestClient

from support.app import Desk, create
from support.callbacks import Callbacks
from support.helpcentre import ARTICLES
from support.tickets import Ticket, parcel_of
from support.photos import Uploads
from support.voicemail import Mailbox, caller


class FakeTickets:
    def __init__(self):
        self.opened = []

    def open(self, channel, customer, message, article, draft):
        t = Ticket(
            f"SUP-{1001 + len(self.opened)}",
            channel,
            customer,
            parcel_of(message),
            message,
            article,
            draft,
            "DRAFTED" if draft else "NEW",
            datetime(2026, 10, 23, 9, 30, tzinfo=UTC),
        )
        self.opened.append(t)
        return t

    def get(self, ticket_id):
        return next((t for t in self.opened if t.id == ticket_id), None)

    def of_parcel(self, parcel):
        return next((t for t in reversed(self.opened) if t.parcel == parcel), None)

    def photo(self, ticket_id, photo, shows, score, status):
        t = self.get(ticket_id)
        t.photo_shows, t.photo_score, t.status = shows, score, status
        return t

    def label(self, ticket_id, reference):
        t = self.get(ticket_id)
        t.label = reference
        return t


class FakeHelpCentre:
    def answer(self, message):
        return ARTICLES[0] if "delivery address" in message else None


class FakeDrafter:
    def draft(self, article, message):
        return f"About {article.title}: {message}"


class FakePhotoCheck:
    def check(self, photo):
        return ("carton", 0.912) if photo == b"box" else ("web site", 0.88)


class FakeLabelReader:
    def read(self, photo):
        return photo.decode()


class FakeTranscriber:
    def transcribe(self, recording):
        return f"Where is PX-5101? ({recording.name})"


def desk():
    return Desk(FakeTickets(), FakeHelpCentre(), FakeDrafter(), FakeTranscriber(), FakePhotoCheck())


def test_a_parcel_is_found_in_a_message():
    assert parcel_of("Can I change the delivery address of PX-5102?") == "PX-5102"
    assert parcel_of("My parcel never came.") is None


def test_a_message_the_help_centre_answers_gets_a_draft():
    d = desk()
    t = d.open("message", "anna.weber@example.com", "Can I change the delivery address of PX-5102?")
    assert (t.article, t.status, t.parcel) == ("change-delivery-address", "DRAFTED", "PX-5102")
    assert t.draft.startswith("About Changing the delivery address")
    t = d.open("message", "tom@example.com", "Do you collect on Sundays?")
    assert (t.article, t.draft, t.status) == (None, None, "NEW")


def test_the_api_opens_a_ticket_of_a_message():
    with TestClient(create(desk)) as client:
        r = client.post("/api/messages", json={"customer": "anna.weber@example.com", "text": "A new delivery address for PX-5102"})
        assert r.status_code == 201
        ticket = r.json()
        assert ticket["status"] == "DRAFTED" and ticket["createdAt"] == "2026-10-23T09:30:00+00:00"
        assert client.get(f"/api/tickets/{ticket['id']}").json() == ticket
        assert client.get("/api/tickets/SUP-1").status_code == 404
        # Its example is the request's, in its OpenAPI document.
        content = client.get("/openapi.json").json()["paths"]["/api/messages"]["post"]["requestBody"]["content"]
        assert content["application/json"]["examples"]["question"]["value"]["customer"] == "anna.weber@example.com"


def test_a_photo_of_the_parcel_makes_its_ticket_ready_for_a_claim():
    d = desk()
    t = d.open("message", "lena.koch@example.com", "PX-5103 arrived damaged")
    assert d.photo("PX-5103", b"order") is True
    assert (t.photo_shows, t.status) == ("web site", "NEW")
    assert d.photo("PX-5103", b"box") is True
    assert (t.photo_shows, t.status) == ("carton", "READY_FOR_CLAIM")
    assert t.json()["photo"] == {"shows": "carton", "score": 0.91}
    assert d.photo("PX-5199", b"box") is False


def test_a_photo_is_kept_or_set_aside_from_the_uploads(tmp_path: Path):
    for parcel in ("PX-5103", "PX-5199"):
        (tmp_path / parcel).mkdir()
        (tmp_path / parcel / "porch.png").write_bytes(b"box")
    (tmp_path / "PX-5103" / "notes.txt").write_text("not a photo")
    taken = []

    def take(parcel, photo):
        taken.append(parcel)
        return parcel == "PX-5103"

    uploads = Uploads(str(tmp_path), take)
    uploads.poll()
    assert taken == []  # it may still be being written
    uploads.poll()
    assert taken == ["PX-5103", "PX-5199"]
    assert (tmp_path / "kept" / "PX-5103" / "porch.png").exists()
    assert (tmp_path / "unmatched" / "PX-5199" / "porch.png").exists()
    assert (tmp_path / "PX-5103" / "notes.txt").exists()
    uploads.poll()
    assert taken == ["PX-5103", "PX-5199"]


class FakeReranker:
    def __init__(self, relevance):
        self.relevance, self.read = relevance, []

    def scores(self, query, documents):
        self.read.append(documents)
        return [self.relevance.get(d.split("\n")[0], 0.0) for d in documents]


def help_centre(reranker, similarity):
    """A help centre of the articles' embeddings as the test gives them."""
    import numpy as np

    from support.helpcentre import HelpCentre

    hc = HelpCentre.__new__(HelpCentre)
    hc.articles, hc.reranker = ARTICLES, reranker
    hc.vectors = np.eye(len(ARTICLES))

    class Model:
        def encode(self, texts):
            return np.array([similarity])

    hc.model = Model()
    return hc


def test_the_reranker_reads_the_near_articles_and_picks_the_relevant_one():
    reranker = FakeReranker({"Delivered, but never received": 0.9, "Changing the delivery address": 0.2})
    hc = help_centre(reranker, [0.8, 0.7, 0.1, 0.0])
    assert hc.answer("Where is my parcel?").slug == "delivered-not-received"
    # It read the two near enough, the nearest first.
    assert [d.split("\n")[0] for d in reranker.read[0]] == ["Changing the delivery address", "Delivered, but never received"]


def test_a_candidate_the_reranker_finds_irrelevant_answers_nothing():
    hc = help_centre(FakeReranker({"Customs charges on parcels from abroad": 0.06}), [0.0, 0.0, 0.0, 1.0])
    assert hc.answer("Do customs charges apply to a business account?") is None
    # Without a reranker, the nearest answers.
    assert help_centre(None, [0.0, 0.0, 0.0, 1.0]).answer("Customs charges?").slug == "customs-charges"
    # No candidate near enough: the reranker reads nothing.
    reranker = FakeReranker({})
    assert help_centre(reranker, [0.1, 0.2, 0.3, 0.4]).answer("Hello") is None and reranker.read == []


def test_a_callers_number_is_read_from_a_messages_details():
    assert caller('[message]\ncallerid="Anna Weber" <+493415550101>\norigtime=1792750500\n') == "+493415550101"
    assert caller("[message]\ncallerid=+493415550177\n") == "+493415550177"
    assert caller("[message]\norigtime=1792750500\n") == "unknown"


def test_a_voicemail_is_taken_once_it_is_written_and_moved_to_old(tmp_path: Path):
    inbox = tmp_path / "INBOX"
    inbox.mkdir()
    (inbox / "msg0001.txt").write_text('callerid="Anna Weber" <+493415550101>\n')
    (inbox / "msg0001.wav").write_bytes(b"RIFF")
    taken = []
    mailbox = Mailbox(str(tmp_path), lambda recording, who: taken.append((recording.name, who)))
    mailbox.poll()
    assert taken == []  # it may still be growing
    mailbox.poll()
    assert taken == [("msg0001.wav", "+493415550101")]
    assert sorted(p.name for p in (tmp_path / "Old").iterdir()) == ["msg0001.txt", "msg0001.wav"]
    assert list(inbox.iterdir()) == []


def test_a_voicemail_that_fails_stays_in_the_inbox(tmp_path: Path):
    inbox = tmp_path / "INBOX"
    inbox.mkdir()
    (inbox / "msg0002.wav").write_bytes(b"RIFF")

    def fail(recording, who):
        raise RuntimeError("the transcriber is down")

    mailbox = Mailbox(str(tmp_path), fail)
    mailbox.poll()
    mailbox.poll()
    assert (inbox / "msg0002.wav").exists()


def test_a_voicemail_opens_a_ticket_of_its_transcript(tmp_path: Path):
    d = desk()
    d.voicemail(tmp_path / "msg0003.wav", "+493415550101")
    t = d.tickets.opened[0]
    assert (t.channel, t.customer, t.parcel, t.message) == ("voicemail", "+493415550101", "PX-5101", "Where is PX-5101? (msg0003.wav)")


class FakeSpeaker:
    def __init__(self):
        self.said = []

    def speak(self, text, wav):
        self.said.append(text)
        wav.write_bytes(b"RIFF")


def test_a_voicemail_gets_a_call_back_that_confirms_it(tmp_path: Path):
    speaker = FakeSpeaker()
    d = desk()
    d.callbacks = Callbacks(speaker, str(tmp_path / "spool"), str(tmp_path / "support"), "support-trunk")
    d.voicemail(tmp_path / "msg0003.wav", "+493415550101")
    assert speaker.said == ["Thank you for your message about parcel PX-5101. We will call you back tomorrow."]
    assert (tmp_path / "support" / "msg0003.wav16").exists()
    call = (tmp_path / "spool" / "outgoing" / "msg0003.call").read_text()
    assert "Channel: PJSIP/+493415550101@support-trunk\n" in call
    assert "Application: Playback\nData: support/msg0003\n" in call
    assert list((tmp_path / "spool" / "tmp").iterdir()) == []


def test_a_voicemail_of_no_number_gets_no_call_back(tmp_path: Path):
    speaker = FakeSpeaker()
    Callbacks(speaker, str(tmp_path / "spool"), str(tmp_path / "support"), "support-trunk").queue("msg0004", "unknown", None)
    assert speaker.said == []
    assert not (tmp_path / "spool").exists()


def test_a_photo_of_a_label_is_read_as_a_label(tmp_path: Path):
    (tmp_path / "PX-5105").mkdir()
    (tmp_path / "PX-5105" / "label.png").write_bytes(b"PX-5105")
    (tmp_path / "PX-5105" / "porch.png").write_bytes(b"box")
    taken, labels = [], []
    uploads = Uploads(str(tmp_path), lambda p, b: taken.append(b) or True, lambda p, b: labels.append(b) or False)
    uploads.poll()
    uploads.poll()
    assert (taken, labels) == ([b"box"], [b"PX-5105"])
    assert (tmp_path / "kept" / "PX-5105" / "porch.png").exists()
    assert (tmp_path / "unmatched" / "PX-5105" / "label.png").exists()


def test_a_label_of_the_parcel_confirms_its_ticket():
    d = desk()
    d.label_reader = FakeLabelReader()
    t = d.open("message", "maria@example.com", "My parcel PX-5105 arrived damaged.")
    assert d.label("PX-5105", b"PX-5105")
    assert t.label == "PX-5105"


def test_a_label_of_another_parcel_is_set_aside():
    d = desk()
    d.label_reader = FakeLabelReader()
    t = d.open("message", "paul@example.com", "My parcel PX-5106 arrived damaged.")
    assert not d.label("PX-5106", b"PX-5107")
    assert t.label is None
    assert not d.label("PX-5198", b"PX-5198")  # nobody wrote about it


class FakeScreen:
    def speech(self, recording):
        return recording.name != "msg0004.wav"


def test_a_voicemail_no_one_speaks_in_opens_no_ticket(tmp_path: Path):
    d = desk()
    d.screen = FakeScreen()
    d.voicemail(tmp_path / "msg0004.wav", "+493415550199")
    d.voicemail(tmp_path / "msg0005.wav", "+493415550101")
    assert [t.customer for t in d.tickets.opened] == ["+493415550101"]
