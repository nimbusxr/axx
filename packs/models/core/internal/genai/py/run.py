# Runs language models with ONNX Runtime GenAI (its Python package, the
# library apps run on Windows, Android, iOS and in services) for axx's tests
# of stubs. It reads a JSON list of jobs on stdin, each a model's folder (its
# genai_config.json, model, tokenizer) and chats to answer (a system prompt,
# a user's prompt and how to sample), and writes their answers, as a JSON
# list, on stdout.
import json
import sys

import onnxruntime_genai as og

jobs = json.load(sys.stdin)
results = []
for job in jobs:
    model = og.Model(job["model"])
    tokenizer = og.Tokenizer(model)
    answers = []
    for chat in job["chats"]:
        messages = []
        if chat.get("system"):
            messages.append({"role": "system", "content": chat["system"]})
        messages.append({"role": "user", "content": chat["prompt"]})
        prompt = tokenizer.apply_chat_template(messages=json.dumps(messages), add_generation_prompt=True)
        tokens = tokenizer.encode(prompt)
        params = og.GeneratorParams(model)
        options = {"max_length": len(tokens) + chat.get("maxTokens", 256)}
        if chat.get("temperature", 0) > 0:
            options.update(do_sample=True, temperature=chat["temperature"])
        else:
            options.update(do_sample=False, top_k=1)
        params.set_search_options(**options)
        generator = og.Generator(model, params)
        generator.append_tokens(tokens)
        while not generator.is_done():
            generator.generate_next_token()
        answers.append(tokenizer.decode(generator.get_sequence(0)[len(tokens):]))
        del generator
    results.append({"answers": answers})
json.dump(results, sys.stdout)
