"""Draft replies: a language model (SmolLM2-135M-Instruct) writes a reply to
the customer from the help-centre article that answers their message, on the
engine the deployment chooses (SUPPORT_DRAFTER): transformers, of the model's
own folder (the default), or ONNX Runtime GenAI, of the model exported for it
(its model builder's folder: genai_config.json, model.onnx, the tokenizer).
An agent reads the reply, and sends it or writes another.
"""

import json

from .helpcentre import Article

SYSTEM = (
    "You draft replies for the support team of a parcel delivery company. Reply to the "
    "customer's message in a few friendly sentences, using only what the help-centre "
    "article says. End with: Kind regards, the Parcels support team"
)

# The longest draft, in tokens.
MAX_TOKENS = 200


class Drafter:
    """Writes draft replies."""

    ENGINES = ("transformers", "onnxruntime-genai")

    def __init__(self, model_dir: str, engine: str = "transformers"):
        if engine not in self.ENGINES:
            raise ValueError(f"no drafter engine {engine!r}: one of {', '.join(self.ENGINES)}")
        self.engine = engine
        if engine == "transformers":
            from transformers import AutoModelForCausalLM, AutoTokenizer

            self.tokenizer = AutoTokenizer.from_pretrained(model_dir)
            self.model = AutoModelForCausalLM.from_pretrained(model_dir)
        else:
            import onnxruntime_genai as og

            self.model = og.Model(model_dir)
            self.tokenizer = og.Tokenizer(self.model)

    def draft(self, article: Article, message: str) -> str:
        chat = [
            {"role": "system", "content": SYSTEM},
            {
                "role": "user",
                "content": f"Help-centre article: {article.title}\n{article.body}\n\nThe customer's message:\n{message}",
            },
        ]
        if self.engine == "onnxruntime-genai":
            return self.generate(chat)
        inputs = self.tokenizer.apply_chat_template(
            chat, add_generation_prompt=True, return_tensors="pt", return_dict=True
        )
        out = self.model.generate(**inputs, max_new_tokens=MAX_TOKENS, do_sample=False)
        reply = out[0][inputs["input_ids"].shape[1] :]
        return self.tokenizer.decode(reply, skip_special_tokens=True).strip()

    def generate(self, chat: list[dict]) -> str:
        """A reply, greedy, with ONNX Runtime GenAI."""
        import onnxruntime_genai as og

        prompt = self.tokenizer.apply_chat_template(json.dumps(chat), add_generation_prompt=True)
        ids = self.tokenizer.encode(prompt)
        params = og.GeneratorParams(self.model)
        params.set_search_options(max_length=len(ids) + MAX_TOKENS, do_sample=False)
        generator = og.Generator(self.model, params)
        generator.append_tokens(ids)
        reply = []
        while not generator.is_done():
            generator.generate_next_token()
            reply.append(generator.get_next_tokens()[0])
        return self.tokenizer.decode(reply).strip()
