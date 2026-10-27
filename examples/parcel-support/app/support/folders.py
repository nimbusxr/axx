"""Folders the service watches: other systems write files into them (the
phone system its voicemails, the tracking site its customers' photos), and
the service takes each once it has stopped growing."""

import logging
import threading
from collections.abc import Iterable
from pathlib import Path

log = logging.getLogger("support.folders")


class Watcher:
    """Polls a folder: poll takes what is ready."""

    name = "folder"

    def __init__(self, every: float = 1.0):
        self.every = every
        self.sizes: dict[Path, int] = {}
        self.stopped = threading.Event()

    def start(self) -> None:
        threading.Thread(target=self.run, name=self.name, daemon=True).start()

    def stop(self) -> None:
        self.stopped.set()

    def run(self) -> None:
        while not self.stopped.wait(self.every):
            try:
                self.poll()
            except Exception:
                log.exception("the %s folder could not be read", self.name)

    def poll(self) -> None:
        raise NotImplementedError

    def settled(self, files: Iterable[Path]) -> list[Path]:
        """The files whose size the last poll saw too: written. A file taken
        and left (one that failed) is taken again two polls later."""
        ready, sizes = [], {}
        for f in sorted(files):
            size = f.stat().st_size
            if self.sizes.get(f) == size:
                ready.append(f)
            else:
                sizes[f] = size
        self.sizes = sizes
        return ready
