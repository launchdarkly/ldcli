## Verifying build provenance with GitHub artifact attestations

LaunchDarkly uses [GitHub artifact attestations](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds) to help developers make their supply chain more secure by ensuring the authenticity and build integrity of our published packages.

LaunchDarkly publishes provenance about our package builds using [GitHub's `actions/attest` action](https://github.com/actions/attest). These attestations are stored in GitHub's attestation API and can be verified using the [GitHub CLI](https://cli.github.com/).

To verify build provenance attestations, we recommend using the [GitHub CLI `attestation verify` command](https://cli.github.com/manual/gh_attestation_verify). Example usage for verifying packages for Linux is included below:

<!-- x-release-please-start-version -->
```
# Set the version of the package to verify
PACKAGE_VERSION=0.12.1
```
<!-- x-release-please-end -->

```
# Download the release archive from GitHub
$ curl --location -O \
  https://github.com/launchdarkly/ldcli/releases/download/${PACKAGE_VERSION}/ldcli_${PACKAGE_VERSION}_linux_amd64.tar.gz

# Verify provenance using the GitHub CLI
$ gh attestation verify ldcli_${PACKAGE_VERSION}_linux_amd64.tar.gz --owner launchdarkly
```

You can also verify the provenance of the published container images:

```
$ gh attestation verify oci://launchdarkly/ldcli:${PACKAGE_VERSION} --owner launchdarkly
```

Below is a sample of expected output.

```
Loaded digest sha256:... for file://ldcli_3.0.0_linux_amd64.tar.gz
Loaded 1 attestation from GitHub API

The following policy criteria will be enforced:
- Predicate type must match:................ https://slsa.dev/provenance/v1
- Source Repository Owner URI must match:... https://github.com/launchdarkly
- Subject Alternative Name must match regex: (?i)^https://github.com/launchdarkly/
- OIDC Issuer must match:................... https://token.actions.githubusercontent.com

✓ Verification succeeded!

The following 1 attestation matched the policy criteria

- Attestation #1
  - Build repo:..... launchdarkly/ldcli
  - Build workflow:. .github/workflows/release-please.yml
  - Signer repo:.... launchdarkly/ldcli
  - Signer workflow: .github/workflows/release-please.yml
```

For more information, see [GitHub's documentation on verifying artifact attestations](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds#verifying-artifact-attestations-with-the-github-cli).

**Note:** These instructions do not apply when building our CLI from source.
