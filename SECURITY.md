# Security policy

## Reporting a vulnerability

Please do not open a public issue for security problems. Write to the address on
https://getartecraft.com (contact page) with:

- version of ArteCraft and your platform
- what happens and how to reproduce it
- the impact you expect

You will get an answer as soon as possible, and a fix in the next build. Credit is given in the
release notes if you want it.

## What the launcher guarantees

- downloads are allowed only from `github.com`, `api.github.com` and `*.githubusercontent.com`;
  every redirect hop is validated, so a compromised release page cannot point the launcher at
  another host
- each downloaded file is hashed with SHA-256 and the hash is shown in the UI
- archives are extracted inside the installation folder; there is no installation into the
  system package manager
- the UI is served from loopback only and binds to `127.0.0.1` with a random port
- no admin rights are requested on Windows: autostart is written to `HKCU` only

## Out of scope

- programs you install through the launcher: they are third-party software with their own rules
- modified builds: verify the SHA-256 published in the release
