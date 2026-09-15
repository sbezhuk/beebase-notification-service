# Build the Go binary on the runner's native platform; only the final
# runtime image is arm64. This avoids running the Go toolchain under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN --mount=type=secret,id=github_token,required=true \
    GOPRIVATE=github.com/sbezhuk/beebase-common \
    GIT_CONFIG_COUNT=1 \
    GIT_CONFIG_KEY_0="url.https://x-access-token:$(cat /run/secrets/github_token)@github.com/.insteadOf" \
    GIT_CONFIG_VALUE_0="https://github.com/" \
    go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 beebase
COPY --from=builder /out/server /usr/local/bin/server
USER beebase
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
