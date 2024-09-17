# Build the controller binary
FROM ghcr.io/cybozu/golang:1.22-jammy AS build

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
