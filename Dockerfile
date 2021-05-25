ARG BASEIMAGE
ARG GOVERSION
ARG BUILDARCH
ARG PKGNAME

# Build the manager binary
FROM golang:${GOVERSION} as builder
ARG BUILDARCH
ARG PKGNAME

# Copy in the go src
WORKDIR /go/src/github.com/clusternet/clusternet
COPY pkg pkg/
COPY cmd cmd/
COPY go.mod go.mod
COPY go.sum go.sum

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${BUILDARCH} go build -a -o ${PKGNAME} /go/src/github.com/clusternet/clusternet/cmd/${PKGNAME}

# Copy the cmd into a thin image
FROM --platform=linux/$BUILDARCH ${BASEIMAGE}
ARG PKGNAME
WORKDIR /root
COPY --from=builder /go/src/github.com/clusternet/clusternet/${PKGNAME} /usr/local/bin/${PKGNAME}
