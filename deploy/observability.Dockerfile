FROM golang:1.26.10-alpine AS probe
WORKDIR /src
COPY cmd/healthprobe/main.go ./main.go
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /healthprobe main.go

FROM jaegertracing/jaeger:2.12.0 AS jaeger
COPY --from=probe /healthprobe /lumo-healthprobe

FROM ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:0.124.1 AS collector
COPY --from=probe /healthprobe /lumo-healthprobe
