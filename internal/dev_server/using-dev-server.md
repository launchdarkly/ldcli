## What should I use it for?

`dev-server` is intended as a replacement for creating certain kinds of environments in the LD app itself that are not part of a flag's release path e.g. Staging, UAT, QA, Production. The most common examples of this are local development environments and ephemeral CI environments. It can also be used for environments that do not need complex targeting rules and are mostly a snapshot of your staging environment equivalent, such as environments for live testing vendor integrations.

## What should I not use this for?

You should refrain from using `dev-server` for any release path critical environments, as well as environments where complex targeting logic is required. Currently, `dev-server` only supports serving a single variation and supports no targeting logic.
