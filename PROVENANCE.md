## Verifying build provenance with the SLSA framework

LaunchDarkly uses the [SLSA framework](https://slsa.dev/spec/v1.0/about) (Supply-chain Levels for Software Artifacts) to help developers make their supply chain more secure by ensuring the authenticity and build integrity of our published packages.

As part of [SLSA requirements for level 3 compliance](https://slsa.dev/spec/v1.0/requirements), LaunchDarkly publishes provenance about our package builds using [GitHub's generic SLSA3 provenance generator](https://github.com/slsa-framework/slsa-github-generator/blob/main/internal/builders/generic/README.md#generation-of-slsa3-provenance-for-arbitrary-projects) for distribution alongside our packages.

<!-- x-release-please-start-version -->
These attestations are available for download from the GitHub release page for the release version under Assets > `ldcli_0.12.1_multiple_provenance.intoto.jsonl`.
<!-- x-release-please-end -->

To verify SLSA provenance attestations, we recommend using [slsa-verifier](https://github.com/slsa-framework/slsa-verifier). Example usage for verifying packages for Linux is included below: 

<!-- x-release-please-start-version -->
```
# Set the version of the PACKAGE to verify
PACKAGE_VERSION=0.12.1
```
<!-- x-release-please-end -->

```
# Ensure provenance file is downloaded along with packages for your OS
# Run slsa-verifier to verify provenance against package artifacts 
$ slsa-verifier verify-artifact \
--provenance-path ldcli_${PACKAGE_VERSION}_multiple_provenance.intoto.jsonl \
--source-uri github.com/launchdarkly/ldcli \
ldcli_${PACKAGE_VERSION}_*.tar.gz
```

Below is a sample of expected output:
```
Verified signature against tlog entry index 84971628 at URL: https://rekor.sigstore.dev/api/v1/log/entries/24296fb24b8ad77a9053fbc27f7e695f7bcf705e69e3596a48e4759b9f9429725d4fec327c9d09bf
Verified build using builder "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v1.10.0" at commit 50b064100a9a142a6da6539e520deef1df6a4ddf
Verifying artifact ldcli_0.6.0_darwin_amd64.tar.gz: PASSED

Verified signature against tlog entry index 84971628 at URL: https://rekor.sigstore.dev/api/v1/log/entries/24296fb24b8ad77a9053fbc27f7e695f7bcf705e69e3596a48e4759b9f9429725d4fec327c9d09bf
Verified build using builder "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v1.10.0" at commit 50b064100a9a142a6da6539e520deef1df6a4ddf
Verifying artifact ldcli_0.6.0_darwin_arm64.tar.gz: PASSED

Verified signature against tlog entry index 84971628 at URL: https://rekor.sigstore.dev/api/v1/log/entries/24296fb24b8ad77a9053fbc27f7e695f7bcf705e69e3596a48e4759b9f9429725d4fec327c9d09bf
Verified build using builder "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v1.10.0" at commit 50b064100a9a142a6da6539e520deef1df6a4ddf
Verifying artifact ldcli_0.6.0_linux_386.tar.gz: PASSED

Verified signature against tlog entry index 84971628 at URL: https://rekor.sigstore.dev/api/v1/log/entries/24296fb24b8ad77a9053fbc27f7e695f7bcf705e69e3596a48e4759b9f9429725d4fec327c9d09bf
Verified build using builder "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v1.10.0" at commit 50b064100a9a142a6da6539e520deef1df6a4ddf
Verifying artifact ldcli_0.6.0_linux_amd64.tar.gz: PASSED

Verified signature against tlog entry index 84971628 at URL: https://rekor.sigstore.dev/api/v1/log/entries/24296fb24b8ad77a9053fbc27f7e695f7bcf705e69e3596a48e4759b9f9429725d4fec327c9d09bf
Verified build using builder "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v1.10.0" at commit 50b064100a9a142a6da6539e520deef1df6a4ddf
Verifying artifact ldcli_0.6.0_linux_arm64.tar.gz: PASSED

PASSED: Verified SLSA provenance
```

Alternatively, to verify the provenance manually, the SLSA framework specifies [recommendations for verifying build artifacts](https://slsa.dev/spec/v1.0/verifying-artifacts) in their documentation.

**Note:** These instructions do not apply when building our CLI from source. 
