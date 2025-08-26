# Copyright Contributors to the Open Cluster Management project

FROM registry.ci.openshift.org/stolostron/builder:go1.23-linux AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY addon/ addon/

# Build
RUN CGO_ENABLED=1 go build -a -o manager main.go
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

ENV USER_UID=1001 \
    USER_NAME=search-v2-operator

# install operator binary
COPY --from=builder /workspace/manager .
USER ${USER_UID}

ENTRYPOINT ["/manager"]

