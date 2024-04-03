FROM alpine:3.19.1

RUN apk update
RUN apk add --no-cache git

COPY ldcli /ldcli

LABEL homepage="https://www.launchdarkly.com"

ENTRYPOINT ["/ldcli"]
