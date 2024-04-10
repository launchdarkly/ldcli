# LaunchDarkly CLI

The LaunchDarkly CLI helps you manage your feature flags wherever you are, whether that's in the webapp, your terminal, or your IDE.

**With the CLI, you can:**

- Evaluate your first feature flag with a guided setup command.
- Onboard your whole team by inviting new members.
- Interact with the LaunchDarkly API using resource & CRUD based commands.

# 🚫🚫🚫🚫🚫🚫

> [!CAUTION]
> This package is prerelease and experimental. It should not be used in production and is not supported.

![demo](ldcli-setup.gif)

## Installation

The LaunchDarkly CLI will soon be available for macOS, Windows, and Linux.

## Usage

Installing the CLI provides access to the `ldcli` command.

```sh-session
ldcli [command]

# Run `--help` for detailed information about CLI commands
ldcli --help
```

## Commands

LaunchDarkly CLI commands
- `setup` guides you through creating your first flag, connecting an SDK, and evaluating your flag in your Test environment

### Resource Commands
Resource commands mirror the LaunchDarkly API and make requests for a given resource. To see a full list of resources supported by the CLI, enter `ldcli --help` into your terminal, and to see the commands available for a given resource

To see the commands available for a given resource:
```sh-session
ldcli <resource> --help
```

An example command to create a flag:
```sh-session
ldcli flags create --access-token <access-token> --project default --data '{"name": "My Test Flag", "key": "my-test-flag"}'
```

## Documentation

_(coming soon!)_

[//]: # (TODO: add info about how to opt out)


## Contributing

We encourage pull requests and other contributions from the community. Check out our [contributing guidelines](CONTRIBUTING.md) for instructions on how to contribute to this project.

## Verifying build provenance with the SLSA framework

LaunchDarkly uses the [SLSA framework](https://slsa.dev/spec/v1.0/about) (Supply-chain Levels for Software Artifacts) to help developers make their supply chain more secure by ensuring the authenticity and build integrity of our published packages. To learn more, see the [provenance guide](./PROVENANCE.md). 

## About LaunchDarkly

* LaunchDarkly is a continuous delivery platform that provides feature flags as a service and allows developers to iterate quickly and safely. We allow you to easily flag your features and manage them from the LaunchDarkly dashboard.  With LaunchDarkly, you can:
    * Roll out a new feature to a subset of your users (like a group of users who opt-in to a beta tester group), gathering feedback and bug reports from real-world use cases.
    * Gradually roll out a feature to an increasing percentage of users, and track the effect that the feature has on key metrics (for instance, how likely is a user to complete a purchase if they have feature A versus feature B?).
    * Turn off a feature that you realize is causing performance problems in production, without needing to re-deploy, or even restart the application with a changed configuration file.
    * Grant access to certain features based on user attributes, like payment plan (eg: users on the ‘gold’ plan get access to more features than users in the ‘silver’ plan). Disable parts of your application to facilitate maintenance, without taking everything offline.
* LaunchDarkly provides feature flag SDKs for a wide variety of languages and technologies. Read [our documentation](https://docs.launchdarkly.com/sdk) for a complete list.
* Explore LaunchDarkly
    * [launchdarkly.com](https://www.launchdarkly.com/ "LaunchDarkly Main Website") for more information
    * [docs.launchdarkly.com](https://docs.launchdarkly.com/  "LaunchDarkly Documentation") for our documentation and SDK reference guides
    * [apidocs.launchdarkly.com](https://apidocs.launchdarkly.com/  "LaunchDarkly API Documentation") for our API documentation
    * [blog.launchdarkly.com](https://blog.launchdarkly.com/  "LaunchDarkly Blog Documentation") for the latest product updates
