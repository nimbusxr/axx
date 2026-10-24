# Runs LiteRT-LM models with LiteRT-LM (its Python package, of the C++
# engine Android's and iOS's libraries run) for axx's tests of stubs. It
# reads a JSON list of jobs on stdin, each a model's path and conversations,
# each a list of a user's prompts, and writes, as a JSON list on stdout, the
# model's answers: each conversation's, a prompt at a time, in a
# conversation of its own.
import json
import sys

import litert_lm

litert_lm.set_min_log_severity(litert_lm.LogSeverity.ERROR)
jobs = json.load(sys.stdin)
results = []
for job in jobs:
    engine = litert_lm.Engine(job["model"])
    answers = []
    for prompts in job["conversations"]:
        conversation = engine.create_conversation()
        said = []
        for prompt in prompts:
            reply = conversation.send_message(prompt)
            said.append("".join(c.text for c in reply.contents if hasattr(c, "text")))
        answers.append(said)
    results.append(answers)
json.dump(results, sys.stdout)
