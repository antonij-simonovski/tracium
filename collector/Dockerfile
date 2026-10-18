FROM golang:1.27-alpine AS builder
RUN apk --no-cache add ca-certificates git
WORKDIR /src

# Must match otelcol_version in builder/oss.builder.yaml.
ARG OCB_VERSION=v0.116.0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for attempt in 1 2 3 4 5; do \
      go install go.opentelemetry.io/collector/cmd/builder@${OCB_VERSION} && break; \
      sleep $((attempt * 5)); \
    done; \
    command -v builder

COPY . .

# proxy.golang.org intermittently resets streams on busy CI runners; retries resume from the module cache.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for attempt in 1 2 3 4 5; do \
      builder --config builder/oss.builder.yaml && break; \
      sleep $((attempt * 5)); \
    done; \
    test -x _build/tracium-collector

FROM alpine:3.24
RUN apk --no-cache add ca-certificates \
    && mkdir -p /var/lib/tracium/queue \
    && chown 10001:10001 /var/lib/tracium/queue
WORKDIR /app
COPY --from=builder /src/_build/tracium-collector /app/tracium-collector
COPY config/collector.yaml /etc/tracium/collector.yaml
USER 10001:10001
EXPOSE 4317 4318 8080
ENTRYPOINT ["/app/tracium-collector"]
CMD ["--config", "/etc/tracium/collector.yaml"]
