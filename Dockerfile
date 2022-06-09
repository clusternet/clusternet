ARG BASEIMAGE
ARG GOVERSION
ARG LDFLAGS
ARG PKGNAME

# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:${GOVERSION} as builder
# Copy in the go src
WORKDIR /go/src/github.com/clusternet/clusternet
COPY pkg pkg/
COPY cmd cmd/
COPY go.mod go.mod
COPY go.sum go.sum

ARG LDFLAGS
ARG PKGNAME
ARG TARGETOS
ARG TARGETARCH

# Build
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="${LDFLAGS}" -a -o ${PKGNAME} /go/src/github.com/clusternet/clusternet/cmd/${PKGNAME}

# Copy the cmd into a thin image
FROM ${BASEIMAGE}
WORKDIR /root
RUN apk add gcompat
ARG PKGNAME
COPY --from=builder /go/src/github.com/clusternet/clusternet/${PKGNAME} /usr/local/bin/${PKGNAME}
