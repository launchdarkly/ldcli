# Dev Server UI

The dev server UI is a very small react app that is used to view the flags & flag variations that the dev server will serve.

**NOTE: be sure to run all commands in the `internal/ui` directory**

## Prerequisites

This uses `vite` for builds & `npm` to manage dependencies. `npm i` to install dependencies.

## Build

Because the UI is modified far less frequently than the rest of the CLI, we build the UI locally so that we don't need the JS toolchain in CI.

To update the UI that is served by the dev sever, run `npm run build` and rebuild the go server. Be sure to check in the update in `dist` so that your changes are incorporated into the go build

## Run

When developing, you can use `npm run dev` to spin up a development version of the UI. Be sure to also run the dev server to provide the APIs.
