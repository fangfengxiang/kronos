FROM golang:1.25.0-alpine3.22@sha256:f18a072054848d87a8077455f0ac8a25886f2397f88bfdd222d6fafbb5bba440 AS build

WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off
COPY go.mod go.sum ./
COPY core/ core/
COPY framework/ framework/
COPY infra/ infra/
COPY platform/kronos/ platform/kronos/
COPY examples/assistant/ examples/assistant/
COPY examples/textbackend/ examples/textbackend/
RUN go build -o /out/assistant ./examples/assistant/cmd/assistant

FROM alpine:3.22.8@sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f

COPY --from=build /out/assistant /usr/local/bin/assistant
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/assistant"]
