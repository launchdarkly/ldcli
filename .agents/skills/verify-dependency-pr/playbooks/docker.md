# Playbook: Docker base image (`Dockerfile.goreleaser`)

The published image is `FROM alpine:<tag>` plus the static `ldcli` binary from goreleaser. Images are built only at release (`.goreleaser.yaml` `dockers`, through the publish composite). PR CI never builds the image.

## Baseline (from `verify.sh`)

`docker-image` checks that the new tag resolves on Docker Hub. When Docker is available, it also builds `Dockerfile.goreleaser` with a static, CGO-free `ldcli` and runs `--version` inside the image. Without Docker it is **skipped**. It is a required check, so the verdict becomes "needs human" with the reason spelled out. `binary-smoke` runs too.

## Impact analysis

- Read the Alpine release notes for each minor between `from` and `to`: musl changes, removed packages, CA bundle, and busybox. ldcli is statically linked, so it only needs the kernel ABI and `/etc/ssl` certificates (for HTTPS to LaunchDarkly).
- Check the support status: Alpine branches get about 2 years of security fixes. Moving off an EOL branch (3.19 went EOL in 2025-11) is a security improvement worth saying out loud.
- If a scanner is available, compare CVE counts for the old and new image: `docker scout cves alpine:<tag>`, or `trivy image`.

## Generated-check ideas (need Docker)

- Guard: in the built image, `ldcli --version` works, `ldcli flags list --help` works, and `wget -q -O- https://app.launchdarkly.com` (busybox) validates TLS against the image's CA bundle.
- Guard: with the full profile, the multi-arch images (`linux/arm64`, `arm/v7`, `386`) build in `release-snapshot`.
