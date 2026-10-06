# The depot desk's Linux builds and the desktops axx runs them on (X11, and GNOME Shell on
# Wayland), in one image: run.sh builds a build and runs the feature against it. CI runs it
# from its registry, the image built once for each version of this file: a run anywhere sees
# the same libraries, and draws the same pixels.
FROM ubuntu:24.04@sha256:534baea6a22c03a63003dbc8dbe78fe34bc0d7e595d9a9dc9834884ff530eb55
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git rsync file xz-utils unzip zip \
      dbus dbus-x11 dbus-user-session at-spi2-core fonts-dejavu-core \
      xvfb x11-utils \
      gnome-shell mutter pipewire wireplumber xwayland xdg-desktop-portal xdg-desktop-portal-gnome \
      python3-gi gir1.2-gtk-3.0 gir1.2-gtk-4.0 python3-pyqt5 python3-pyqt6 qt6-wayland \
      openjdk-21-jdk libatk-wrapper-java libatk-wrapper-java-jni \
      libnss3 libasound2t64 libgbm1 libxss1 \
      build-essential pkg-config libwebkit2gtk-4.1-dev libxdo-dev libssl-dev librsvg2-dev \
      clang cmake ninja-build libgtk-3-dev libglu1-mesa \
    && rm -rf /var/lib/apt/lists/*
# Node.js 22 (Electron's installer needs a newer one than Ubuntu's), Rust and Flutter, pinned.
ARG NODE=22.23.3 RUST=1.99.0 FLUTTER=3.47.6
RUN arch=$(dpkg --print-architecture | sed 's/amd64/x64/') \
    && curl -fsSL "https://nodejs.org/dist/v$NODE/node-v$NODE-linux-$arch.tar.xz" | tar -xJ -C /opt \
    && ln -s "/opt/node-v$NODE-linux-$arch/bin/"* /usr/local/bin/
RUN curl -sSf https://sh.rustup.rs | sh -s -- -y --profile minimal --default-toolchain "$RUST"
ENV PATH=/root/.cargo/bin:/opt/flutter/bin:$PATH
RUN git clone --quiet --depth 1 --branch "$FLUTTER" https://github.com/flutter/flutter.git /opt/flutter \
    && flutter config --no-analytics --enable-linux-desktop && flutter precache --linux
COPY run.sh /usr/local/bin/depot-desk
