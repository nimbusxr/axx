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
# The system's models the desk's GTK builds use (../README.md): Tesseract, and the desktop's
# speech service, speech-dispatcher, with its audio plugins (ALSA's null device, where a desktop
# without sound plays its speech), which speaks with Piper's release (its CLI, with espeak-ng's
# data), pinned by its sums. The locale axx gives the apps (en_US.UTF-8), which speech-dispatcher
# does not start without.
ARG PIPER=2023.11.14-2
RUN apt-get update && apt-get install -y --no-install-recommends \
      locales tesseract-ocr speech-dispatcher speech-dispatcher-audio-plugins python3-speechd \
    && rm -rf /var/lib/apt/lists/* \
    && locale-gen en_US.UTF-8 \
    && case $(dpkg --print-architecture) in \
         arm64) piper=aarch64 sum=fea0fd2d87c54dbc7078d0f878289f404bd4d6eea6e7444a77835d1537ab88eb ;; \
         amd64) piper=x86_64 sum=a50cb45f355b7af1f6d758c1b360717877ba0a398cc8cbe6d2a7a3a26e225992 ;; \
       esac \
    && curl -fsSL -o /tmp/piper.tar.gz "https://github.com/rhasspy/piper/releases/download/$PIPER/piper_linux_$piper.tar.gz" \
    && echo "$sum  /tmp/piper.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/piper.tar.gz -C /opt && rm /tmp/piper.tar.gz \
    && ln -s /opt/piper/piper /usr/local/bin/piper
COPY run.sh /usr/local/bin/depot-desk
