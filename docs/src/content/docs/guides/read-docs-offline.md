---
title: Read the docs offline
description: Serve Axx's documentation on your own machine with axx docs serve, or host a copy for your organization, when your network can't reach axx.nimbusxr.us.
---

Some networks block axx.nimbusxr.us, and some machines can't reach the internet at all. Every release of Axx publishes its documentation as an archive, `axx_<version>_docs.tar.gz`, beside its binaries on GitHub. You can read that copy two ways:

- serve it on your own machine with `axx docs serve`;
- host it on a web server inside your organization.

The copy is the whole site, search included, as it was for that release. It loads nothing from other hosts, so it works with no internet access.

## Serve the docs on your machine

1. Run `axx docs serve` with `--open` to open the docs in your browser:

   ```sh
   axx docs serve --open
   ```

   It prints the address, `http://127.0.0.1:7357/`, and serves the docs until you press Ctrl-C.

2. The first time, Axx downloads the docs archive of the release you run from GitHub. It checks the download against the release's `checksums.txt`, as the install script does, and keeps the docs in its cache, so later runs work offline.

The docs are those of the Axx you run. In a project that [names its Axx version](/guides/install/#keep-a-project-on-one-version), they are that version's docs, which can differ from the newest release's on axx.nimbusxr.us.

Coding agents can read the local copy too. A request that prefers Markdown (`Accept: text/markdown`) gets each page as Markdown, as it does from axx.nimbusxr.us.

### Choose the port

The docs are served on port 7357, which common developer tools don't use. To serve them on another port:

- pass `--port`, like `axx docs serve --port 9123`. `--port 0` takes any free port, and Axx prints the address it took.
- or set `docs.port` in the project's `axx.yaml`, so everyone on the project gets the same port:

  ```yaml
  docs:
    port: 9123
  ```

`--port` wins over `docs.port`. If another program already has the port, `axx docs serve` stops with an error ([AXX-E0026](/references/error-codes/#axx-e0026)) instead of picking another port by itself, so the address stays the same from one run to the next.

The docs are served on `127.0.0.1` only, so other machines on your network can't reach them.

### When the machine can't reach GitHub

`axx docs serve` downloads from GitHub, like `axx upgrade` and the install script. If your machine can't reach GitHub either, use one of these:

- **A mirror.** If your organization mirrors Axx's releases, set `AXX_DOWNLOAD_URL` to the mirror, as for the binaries. Axx looks for `<mirror>/v<version>/checksums.txt` and the docs archive beside it. [Use your organization's mirrors](/guides/use-mirrors/) sets up mirrors for everything Axx downloads.
- **A copy of the archive.** Download `axx_<version>_docs.tar.gz` from the release's page on a machine that can, copy it over, and serve it with `--from`:

  ```sh
  axx docs serve --from axx_0.7.0_docs.tar.gz
  ```

  `--from` also serves a folder that holds a built site, such as `docs/dist` after `npm run build` in Axx's repository.

## Host the docs for your organization

To give everyone in your organization one address for the docs:

1. Download `axx_<version>_docs.tar.gz` from the release's page on GitHub, and check it against the release's `checksums.txt`.
2. Unpack it into the folder a web server serves:

   ```sh
   mkdir axx-docs
   tar -xzf axx_0.7.0_docs.tar.gz -C axx-docs
   ```

3. Serve the folder at the root of its own host, like `https://axx-docs.example.com`. The site's pages link to each other from the root (`/guides/install/`), so they don't work from a path below it, like `https://docs.example.com/axx/`.

Any static web server works: it serves a folder's `index.html` for the folder's address, and the site's `404.html` for an address that doesn't exist. To also give coding agents Markdown, as axx.nimbusxr.us does, serve `<page>.md` (the file beside the page's folder) to a request that prefers `text/markdown`.

When a new release comes out, unpack its archive the same way to update the copy.

## Point Axx's links at your copy

Axx's error messages end with a link to the page that explains them. Its help and a new project's `axx.yaml` link to the docs too. By default these links lead to axx.nimbusxr.us. To make them lead to your copy:

- set the `AXX_DOCS_URL` environment variable, for every project on a machine or in CI:

  ```sh
  export AXX_DOCS_URL=https://axx-docs.example.com
  ```

- or set `docs.url` in a project's `axx.yaml`:

  ```yaml
  docs:
    url: https://axx-docs.example.com
  ```

`AXX_DOCS_URL` wins over `docs.url`. Either can be `http://127.0.0.1:7357` to lead to `axx docs serve`. With the setting, an error ends like this:

```
error[AXX-E0001]: axx.yaml declares no services: an axx host has nothing to start
  hint: declare the services runs start under services: in axx.yaml
  docs: https://axx-docs.example.com/references/error-codes/#axx-e0001
```

`axx explain AXX-E0001` explains an error code without any site.
