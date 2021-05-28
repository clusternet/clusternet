ARG BASEIMAGE
ARG GOVERSION
ARG GOARCH
ARG CGO_ENABLED
ARG CC
ARG CCPKG
ARG LDFLAGS
ARG PKGNAME
ARG PLATFORM

# Build the manager binary
FROM golang:${GOVERSION} as builder
ARG GOARCH
ARG CGO_ENABLED
ARG CC
ARG CCPKG
ARG LDFLAGS
ARG PKGNAME

# Copy in the go src
WORKDIR /go/src/github.com/clusternet/clusternet
COPY pkg pkg/
COPY cmd cmd/
COPY go.mod go.mod
COPY go.sum go.sum

# Build
RUN test -z ${CCPKG} || (apt-get update && apt-get install -y $CCPKG)
RUN CGO_ENABLED=${CGO_ENABLED} CC=${CC} GOOS=linux GOARCH=${GOARCH} go build -ldflags="${LDFLAGS}" -a -o ${PKGNAME} /go/src/github.com/clusternet/clusternet/cmd/${PKGNAME}

# Copy the cmd into a thin image
FROM --platform=${PLATFORM} ${BASEIMAGE}
ARG PKGNAME
WORKDIR /root
COPY --from=builder /go/src/github.com/clusternet/clusternet/${PKGNAME} /usr/local/bin/${PKGNAME}
