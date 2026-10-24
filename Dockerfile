FROM golang:1.26.10-alpine AS build
RUN apk add --no-cache ca-certificates git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway

FROM alpine:3.22.1
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 10001 app && adduser -D -u 10001 -G app app
WORKDIR /app
COPY --from=build /out/server /out/gateway /app/
USER 10001:10001
EXPOSE 9000 8080
ENTRYPOINT ["/app/server"]
