"""The help centre: its articles, and the search that finds the one that
answers a customer's message.

The search embeds every article when the service starts, and a message as it
arrives, with a sentence embedding model (all-MiniLM-L6-v2), on the engine
the deployment chooses (SUPPORT_HELP_CENTRE): sentence-transformers, of the
model's own folder (the default), or llama.cpp, of a GGUF of it: the
articles near enough the message are its candidates. A reranker (Qwen3-Reranker-0.6B, which llama.cpp
runs in the service) reads the message with each candidate, and the one it
finds relevant answers it. Without a reranker, the nearest candidate does.
"""

import threading
from dataclasses import dataclass
from pathlib import Path

import numpy as np


@dataclass(frozen=True)
class Article:
    slug: str
    title: str
    body: str


ARTICLES = [
    Article(
        "change-delivery-address",
        "Changing the delivery address",
        "You can send a parcel somewhere else until it is out for delivery. Open the "
        "parcel's tracking page, choose Change delivery and enter the new address. A "
        "parcel that is already on its way to you goes to the address on its label.",
    ),
    Article(
        "delivered-not-received",
        "Delivered, but never received",
        "If the tracking page says your parcel was delivered and you don't have it, "
        "check with your neighbours and around your door first: the courier's photo of "
        "the delivery is on the tracking page. If you still cannot find it, tell us, and "
        "we open an investigation with the courier within one working day.",
    ),
    Article(
        "arrived-damaged",
        "Your parcel arrived damaged",
        "Keep the parcel and everything that came in it, and take photos of the box and "
        "its contents. The shop that sent it files the claim with us: send them the "
        "photos within seven days.",
    ),
    Article(
        "customs-charges",
        "Customs charges on parcels from abroad",
        "A parcel from outside the EU may owe duties and import VAT. We pay them at the "
        "border and collect them from you before we deliver the parcel; its tracking "
        "page says how much is due and how to pay.",
    ),
]

# How similar (their embeddings' cosine) a message and an article must be for
# the article to answer the message, a candidate's; and how relevant the
# reranker must find a candidate.
SIMILARITY = 0.5
RELEVANCE = 0.5
# How many candidates the reranker reads.
CANDIDATES = 3


class Reranker:
    """A reranker (a GGUF of rank pooling), which llama.cpp runs in the
    service, through llama-cpp-python: it reads a message and a document as
    the model's own rerank template puts them, and gives the document's
    relevance, the rank llama.cpp pools."""

    def __init__(self, path: str):
        import llama_cpp

        self.lc = llama_cpp
        self.llm = llama_cpp.Llama(
            model_path=path,
            embedding=True,
            pooling_type=llama_cpp.LLAMA_POOLING_TYPE_RANK,
            n_ctx=1024,
            n_batch=1024,
            n_ubatch=1024,
            verbose=False,
        )
        self.template = self.llm.metadata["tokenizer.chat_template.rerank"]
        self.lock = threading.Lock()

    def scores(self, query: str, documents: list[str]) -> list[float]:
        lc, out = self.lc, []
        with self.lock:
            for document in documents:
                prompt = self.template.replace("{query}", query).replace("{document}", document)
                tokens = self.llm.tokenize(prompt.encode(), add_bos=True, special=True)
                lc.llama_memory_clear(lc.llama_get_memory(self.llm.ctx), True)
                # One sequence, every token an output: llama.cpp pools them.
                batch = lc.llama_batch_init(len(tokens), 0, 1)
                try:
                    for i, t in enumerate(tokens):
                        batch.token[i], batch.pos[i], batch.n_seq_id[i], batch.logits[i] = t, i, 1, True
                        batch.seq_id[i][0] = 0
                    batch.n_tokens = len(tokens)
                    if lc.llama_decode(self.llm.ctx, batch) != 0:
                        raise RuntimeError("llama.cpp cannot read the message with the document")
                finally:
                    lc.llama_batch_free(batch)
                out.append(float(lc.llama_get_embeddings_seq(self.llm.ctx, 0)[0]))
        return out


def text(article: Article) -> str:
    return f"{article.title}\n\n{article.body}"


class Embedder:
    """Embeds texts, normalized, on the engine of the model's folder:
    sentence-transformers, or llama.cpp (llama-cpp-python), of the GGUF in
    it, its tokens pooled as the model pools them."""

    ENGINES = ("sentence-transformers", "llama.cpp")

    def __init__(self, model_dir: str, engine: str = "sentence-transformers"):
        if engine not in self.ENGINES:
            raise ValueError(f"no help centre engine {engine!r}: one of {', '.join(self.ENGINES)}")
        self.engine = engine
        if engine == "sentence-transformers":
            from sentence_transformers import SentenceTransformer

            self.model = SentenceTransformer(model_dir, device="cpu")
        else:
            import llama_cpp

            self.model = llama_cpp.Llama(
                model_path=str(next(Path(model_dir).glob("*.gguf"))),
                embedding=True,
                n_ctx=512,
                n_batch=512,
                n_ubatch=512,
                verbose=False,
            )
            self.lock = threading.Lock()

    def encode(self, texts: list[str]) -> np.ndarray:
        if self.engine == "sentence-transformers":
            return self.model.encode(texts, normalize_embeddings=True)
        with self.lock:
            return np.array(self.model.embed(texts, normalize=True))


class HelpCentre:
    """Finds the article that answers a message."""

    def __init__(self, model: Embedder, reranker: Reranker | None = None, articles: list[Article] = ARTICLES):
        self.model = model
        self.reranker = reranker
        self.articles = articles
        self.vectors = self.model.encode([text(a) for a in articles])

    def answer(self, message: str) -> Article | None:
        vector = self.model.encode([message])[0]
        similarity = self.vectors @ vector
        candidates = [int(i) for i in similarity.argsort()[::-1][:CANDIDATES] if similarity[i] >= SIMILARITY]
        if not candidates:
            return None
        if self.reranker is None:
            return self.articles[candidates[0]]
        relevance = self.reranker.scores(message, [text(self.articles[i]) for i in candidates])
        best = max(range(len(candidates)), key=lambda j: relevance[j])
        if relevance[best] < RELEVANCE:
            return None
        return self.articles[candidates[best]]
