# Build the controller binary
FROM ghcr.io/cybozu/golang:1.26.2.2_noble@sha256:8154343edf8b656f4851572321483b8f304fa00b9966f1db687a7c1a3f234da8 AS build

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY internal/ internal/

RUN CGO_ENABLED=0 go install -ldflags="-w -s" ./cmd/...

# Build the local-session-tracker binary
FROM scratch AS local-session-tracker
LABEL org.opencontainers.image.source="https://github.com/cybozu-go/login-protector"

COPY --from=build /go/bin/local-session-tracker .
USER 10000:10000
ENTRYPOINT ["/local-session-tracker"]

# Build the login-protector binary
FROM scratch AS login-protector
LABEL org.opencontainers.image.source="https://github.com/cybozu-go/login-protector"

COPY --from=build /go/bin/login-protector .
USER 10000:10000
ENTRYPOINT ["/login-protector"]
