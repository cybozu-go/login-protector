# Build the controller binary
FROM ghcr.io/cybozu/golang:1.27.3.2_noble@sha256:d78937fddb26c4350824d4022f5c9dfe20bf762811ea31065f8c6d467c58bc30 AS build

WORKDIR /workspace

ENV GOPROXY=https://golang.flatt.tech
ENV NETRC=/run/secrets/netrc

# Copy the Go Modules manifests
COPY go.mod go.sum ./

# Download dependencies through Takumi Guard.
RUN --mount=type=secret,id=netrc,target=/run/secrets/netrc,required=false \
    go mod download

# Copy the go source
COPY cmd/ cmd/
COPY internal/ internal/

# All dependencies were downloaded above.
RUN CGO_ENABLED=0 GOPROXY=off go install -ldflags="-w -s" ./cmd/...

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
