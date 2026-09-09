# syntax=docker/dockerfile:1
# One image builds all Goonj Go binaries; compose picks the entrypoint per service.

FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/ws-gateway ./cmd/ws-gateway \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/seed ./cmd/seed

FROM alpine:3.20
RUN adduser -D -u 10001 goonj
USER goonj
COPY --from=build /out/ /usr/local/bin/
# Migrations travel with the image for the seed/release job.
COPY apps/api/migrations /migrations
ENV MIGRATIONS_DIR=/migrations
