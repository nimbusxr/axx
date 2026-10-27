"""Tickets, in PostgreSQL: one for each message or voicemail a customer
sends. A ticket the help centre answers has the article and a draft reply
(DRAFTED); one it does not is for an agent to answer (NEW). A ticket keeps
the last photo the customer sent of its parcel, and what it shows: a photo
of the parcel makes it ready for the shop's claim (READY_FOR_CLAIM); and the
reference the parcel's label shows, from a photo of it.
"""

import re
from dataclasses import asdict, dataclass
from datetime import datetime

import psycopg
from psycopg.rows import class_row

SCHEMA = """
CREATE SEQUENCE IF NOT EXISTS ticket_numbers START 1001;
CREATE TABLE IF NOT EXISTS tickets (
    id         text PRIMARY KEY,
    channel    text NOT NULL,
    customer   text NOT NULL,
    parcel     text,
    message    text NOT NULL,
    article    text,
    draft      text,
    status     text NOT NULL,
    photo      bytea,
    photo_shows text,
    photo_score real,
    label      text,
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS label text;
"""

# A ticket's columns, as Ticket has them.
COLUMNS = "id, channel, customer, parcel, message, article, draft, status, created_at, photo_shows, photo_score, label"

# A parcel's reference, in a message: PX-5101.
PARCEL = re.compile(r"\bPX-\d{4,}\b")


@dataclass
class Ticket:
    id: str
    channel: str  # message, voicemail
    customer: str  # an e-mail address, or a caller's number
    parcel: str | None
    message: str
    article: str | None
    draft: str | None
    status: str  # DRAFTED, NEW, READY_FOR_CLAIM
    created_at: datetime
    photo_shows: str | None = None
    photo_score: float | None = None
    label: str | None = None  # the reference its parcel's label shows

    def json(self) -> dict:
        d = asdict(self)
        d["createdAt"] = d.pop("created_at").isoformat()
        shows, score = d.pop("photo_shows"), d.pop("photo_score")
        d["photo"] = {"shows": shows, "score": round(score, 2)} if shows else None
        return d


def parcel_of(message: str) -> str | None:
    m = PARCEL.search(message)
    return m.group(0) if m else None


class Tickets:
    def __init__(self, url: str):
        self.url = url
        with psycopg.connect(url) as conn:
            conn.execute(SCHEMA)

    def open(self, channel: str, customer: str, message: str, article: str | None, draft: str | None) -> Ticket:
        status = "DRAFTED" if draft else "NEW"
        with psycopg.connect(self.url) as conn, conn.cursor(row_factory=class_row(Ticket)) as cur:
            cur.execute(
                f"""INSERT INTO tickets (id, channel, customer, parcel, message, article, draft, status)
                    VALUES ('SUP-' || nextval('ticket_numbers'), %s, %s, %s, %s, %s, %s, %s)
                    RETURNING {COLUMNS}""",
                (channel, customer, parcel_of(message), message, article, draft, status),
            )
            return cur.fetchone()

    def get(self, ticket_id: str) -> Ticket | None:
        with psycopg.connect(self.url) as conn, conn.cursor(row_factory=class_row(Ticket)) as cur:
            cur.execute(f"SELECT {COLUMNS} FROM tickets WHERE id = %s", (ticket_id,))
            return cur.fetchone()

    def of_parcel(self, parcel: str) -> Ticket | None:
        """The parcel's latest ticket."""
        with psycopg.connect(self.url) as conn, conn.cursor(row_factory=class_row(Ticket)) as cur:
            cur.execute(f"SELECT {COLUMNS} FROM tickets WHERE parcel = %s ORDER BY created_at DESC LIMIT 1", (parcel,))
            return cur.fetchone()

    def label(self, ticket_id: str, reference: str) -> Ticket:
        with psycopg.connect(self.url) as conn, conn.cursor(row_factory=class_row(Ticket)) as cur:
            cur.execute(f"UPDATE tickets SET label = %s WHERE id = %s RETURNING {COLUMNS}", (reference, ticket_id))
            return cur.fetchone()

    def photo(self, ticket_id: str, photo: bytes, shows: str, score: float, status: str) -> Ticket:
        with psycopg.connect(self.url) as conn, conn.cursor(row_factory=class_row(Ticket)) as cur:
            cur.execute(
                f"""UPDATE tickets SET photo = %s, photo_shows = %s, photo_score = %s, status = %s
                    WHERE id = %s RETURNING {COLUMNS}""",
                (photo, shows, score, status, ticket_id),
            )
            return cur.fetchone()
