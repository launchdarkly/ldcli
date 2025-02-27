# dev server
The dev server is a go server that ldcli can run. It provides a local-only version of all the APIs that support LaunchDarkly SDKs. You can use it to serve flags to local and ephemeral environments. It copies flag _values_ for a project from a source environment and serves those. There are also APIs that let you override those values so that you can enable a feature just in your dev environment.

The build of the dev server is incorporated into the ldcli build itself. The UI provided by the dev server has a [manual build](./ui/README.md).
