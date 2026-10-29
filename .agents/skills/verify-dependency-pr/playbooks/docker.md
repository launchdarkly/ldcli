# Playbook: Docker base image (`Dockerfile.goreleaser`)

The published image is `FROM alpine:<tag>` plus the static `ldcli` binary from goreleaser. The release builds the images. PR CI does not build them.

## Diligent reviewer standard

1. The new tag exists.
2. The release image builds from the PR with a binary made as the release makes it: CGO for SQLite, musl, static link.
3. In the image, `ldcli --version` runs, HTTPS to LaunchDarkly works with the CA bundle of the image, and the dev server starts and serves its API. The dev server uses SQLite, so this step tests CGO on musl.
4. The changes between the two base image versions are known (musl, CA bundle, busybox, end of support).

## What the baseline checks

`docker-image` (gate) covers items 1 to 3. It needs Docker and `musl-gcc` (package `musl-tools`). If one of them is missing, or if a registry lookup or image pull fails, the check reports `incomplete`, not `fail`. `binary-smoke` also runs.

## What the agent must do

- Read the Alpine release notes for each minor version in the range. ldcli links statically, so it needs only the kernel interface and the certificates in `/etc/ssl`.
- Write down the support status. Alpine branches get security fixes for about two years. A move away from a branch that has no more support is a security improvement.
- If a scanner is available (`docker scout cves`, `trivy image`), compare the CVE counts of the old and new image.

## When to ask a person

If the image builds and runs and the impact review is complete, the PR can be safe to merge. Ask a person only if the new base image changes what users see, for example a removed shell or a different user.
