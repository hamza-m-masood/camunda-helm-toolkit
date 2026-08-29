# Multi-stage build: compile with the full Go toolchain, ship a minimal final image.
# Used only by scaffold-watcher's generated CronJob — the CLI itself is normally used
# as a plain binary (see README.md), not from this image.
#
# kubectl and helm are not Alpine packages and are not pulled from an unverified
# third-party base image — both are downloaded directly from their official release
# hosts and checked against the checksum published alongside that exact release.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/camunda-chart-doctor ./cmd/camunda-chart-doctor

FROM alpine:3.20 AS tools
RUN apk add --no-cache curl
ARG KUBECTL_VERSION=v1.37.0
ARG KUBECTL_SHA256=6129359f4e1f3848a5572ccb0b26cf28b8ca08cef38c95a765b2f64a2c961a2f
ARG HELM_VERSION=v4.2.4
ARG HELM_SHA256=c306b46f719b0a4da32d0f78ee21bf90ce8d602f15b22ab753f0674d1670a7f3
RUN set -eu; \
    curl -fsSL -o /tmp/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl"; \
    echo "${KUBECTL_SHA256}  /tmp/kubectl" | sha256sum -c -; \
    chmod +x /tmp/kubectl; \
    curl -fsSL -o /tmp/helm.tar.gz "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz"; \
    echo "${HELM_SHA256}  /tmp/helm.tar.gz" | sha256sum -c -; \
    tar -xzf /tmp/helm.tar.gz -C /tmp; \
    cp /tmp/linux-amd64/helm /tmp/helm-bin

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=tools /tmp/kubectl /usr/local/bin/kubectl
COPY --from=tools /tmp/helm-bin /usr/local/bin/helm
COPY --from=build /out/camunda-chart-doctor /usr/local/bin/camunda-chart-doctor
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/camunda-chart-doctor"]
