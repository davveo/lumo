# MinIO's public prebuilt images are no longer reliably pullable in this
# environment. Build the official release source at an immutable commit.
FROM golang:1.26.10-alpine AS build
RUN apk add --no-cache ca-certificates git
WORKDIR /src/minio
ARG MINIO_COMMIT=07c3a429bfed433e49018cb0f78a52145d4bedeb
RUN git init \
 && git remote add origin https://github.com/minio/minio.git \
 && git fetch --depth=1 origin "$MINIO_COMMIT" \
 && git checkout --detach FETCH_HEAD \
 && test "$(git rev-parse HEAD)" = "$MINIO_COMMIT"
# Official tag RELEASE.2025-09-07T16-13-09Z resolves to the commit above.
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/minio .

FROM alpine:3.22.1
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 10001 app && adduser -D -u 10001 -G app app
COPY --from=build /out/minio /usr/bin/minio
COPY --from=build /src/minio/LICENSE /usr/share/licenses/minio/LICENSE
USER 10001:10001
EXPOSE 9000 9001
ENTRYPOINT ["/usr/bin/minio"]
CMD ["server", "/data", "--console-address", ":9001"]
