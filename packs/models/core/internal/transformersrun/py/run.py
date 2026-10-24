# Runs language models with Hugging Face's transformers (the library
# Python services load them with: AutoModelForCausalLM, and its pipeline)
# for axx's tests of stubs. It reads a JSON list of jobs on stdin, each a
# model's folder (config.json, its weights and its tokenizer's files), a
# system prompt and chats, and writes each job's answers as a JSON list on
# stdout: each chat answered by generate (greedy, or sampled at its
# temperature) of the chat template's prompt, and by the text-generation
# pipeline of the chat's messages. A job of texts to embed (a text embedding
# model's folder) embeds each with sentence-transformers (as its modules
# say: its pooling, its normalization), when the folder is its, and with
# AutoModel, its last hidden state's mean of the text's tokens. A job of
# sounds to transcribe (a speech recognizer's folder: Whisper's) transcribes
# each with the automatic-speech-recognition pipeline, and with its
# timestamps (its chunks, as JSON).
import json
import os
import sys

import torch
from transformers import AutoModel, AutoModelForCausalLM, AutoTokenizer, pipeline


def embed(job):
    from sentence_transformers import SentenceTransformer

    out = {}
    if os.path.exists(os.path.join(job["model"], "modules.json")):
        st = SentenceTransformer(job["model"], device="cpu")
        out["sentences"] = st.encode(job["embed"], convert_to_numpy=True).astype("float64").tolist()
    tokenizer = AutoTokenizer.from_pretrained(job["model"])
    model = AutoModel.from_pretrained(job["model"])
    model.eval()
    means = []
    for text in job["embed"]:
        batch = tokenizer(text, return_tensors="pt")
        with torch.no_grad():
            hidden = model(input_ids=batch["input_ids"], attention_mask=batch["attention_mask"]).last_hidden_state[0]
        mask = batch["attention_mask"][0].unsqueeze(-1).to(hidden.dtype)
        means.append(((hidden * mask).sum(0) / mask.sum()).to(torch.float64).tolist())
    out["means"] = means
    return out


def transcribe(job):
    import numpy as np

    import torch

    dtype = getattr(torch, job.get("dtype") or "float32")
    asr = pipeline("automatic-speech-recognition", model=job["model"], device=job.get("device") or "cpu", dtype=dtype)
    options = {}
    if job.get("language"):
        options["generate_kwargs"] = {"language": job["language"], "task": "transcribe"}
    texts, chunks = [], []
    for samples in job["transcribe"]:
        audio = {"raw": np.array(samples, dtype=np.float32), "sampling_rate": job.get("rate", 16000)}
        texts.append(asr(dict(audio), **options)["text"])
        out = asr(dict(audio), return_timestamps=True, **options)
        chunks.append(json.dumps([{"timestamp": list(c["timestamp"]), "text": c["text"]} for c in out["chunks"]]))
    return {"answers": texts, "pipeline": chunks}


def main():
    jobs = json.load(sys.stdin)
    results = []
    for job in jobs:
        if "embed" in job:
            results.append(embed(job))
            continue
        if "transcribe" in job:
            results.append(transcribe(job))
            continue
        tokenizer = AutoTokenizer.from_pretrained(job["model"])
        model = AutoModelForCausalLM.from_pretrained(job["model"])
        model.eval()
        generator = pipeline("text-generation", model=model, tokenizer=tokenizer)
        answers, piped = [], []
        for chat in job["chats"]:
            messages = [{"role": "system", "content": job["system"]}, {"role": "user", "content": chat["prompt"]}]
            sampled = chat["temperature"] > 0
            options = {"max_new_tokens": chat.get("maxTokens", 64), "do_sample": sampled}
            if sampled:
                options["temperature"] = chat["temperature"]
            torch.manual_seed(0)
            prompt = tokenizer.apply_chat_template(messages, add_generation_prompt=True, return_tensors="pt", return_dict=True)
            with torch.no_grad():
                out = model.generate(**prompt, **options)
            answers.append(tokenizer.decode(out[0, prompt["input_ids"].shape[1]:], skip_special_tokens=True))
            torch.manual_seed(0)
            piped.append(generator(messages, **options)[0]["generated_text"][-1]["content"])
        results.append({"answers": answers, "pipeline": piped})
    json.dump(results, sys.stdout)


if __name__ == "__main__":
    main()
