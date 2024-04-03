FROM alpine:3.19.1

RUN apk update
RUN apk add --no-cache git

COPY ldcli /ldcli

LABEL com.github.actions.name="LaunchDarkly CLI"
LABEL com.github.actions.description="The official command line interface for managing LaunchDarkly feature flags."
LABEL com.github.actions.icon="toggle-right"
LABEL com.github.actions.color="gray-dark"
LABEL homepage="https://www.launchdarkly.com"

ENTRYPOINT ["/ldcli"]
