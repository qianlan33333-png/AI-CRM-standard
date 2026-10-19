# Build on the local Linux builder; the destination never compiles source.
FROM --platform=$BUILDPLATFORM golang:1.26.9-bookworm AS build
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/aicrm ./cmd/aicrm && \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/migrate-platform ./cmd/migrate-platform && \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/migrate-river ./cmd/migrate-river

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata wget && rm -rf /var/lib/apt/lists/* && useradd -u 10001 -m crm
WORKDIR /app
COPY --from=build /out/ /usr/local/bin/
COPY --chown=10001:10001 web/dist/ web/dist/
COPY --chown=10001:10001 migrations/ migrations/
COPY deploy/entrypoint.sh /usr/local/bin/standard-entrypoint
USER 10001:10001
ENTRYPOINT ["standard-entrypoint"]
CMD ["aicrm"]
