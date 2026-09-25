# Security Policy

## Supported versions

During the `0.x` beta, only the latest release receives security fixes.

## Reporting a vulnerability

Please **do not** open a public issue. Report privately through GitHub:
**Security → Report a vulnerability** on <https://github.com/nimbusxr/axx/security>.

We aim to acknowledge reports within 3 business days and to publish a fix and advisory
within 90 days, crediting the reporter unless they prefer otherwise.

## Scope notes

axx executes commands you configure (`apps.*.command`, `cleanup`, readiness `exec` checks) with your
privileges, and it disables TLS verification for the services under test by default, because it
targets local builds. Neither is a vulnerability in itself; please report ways axx runs code or
reaches hosts that the configuration did not ask for.
