# syntax=docker/dockerfile:1.7

FROM golang:1.26 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static binary so the runtime image can stay on distroless/static (no glibc).
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/pack-calculator ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder --chown=nonroot:nonroot /out/pack-calculator /app/pack-calculator

# Pre-create /data with nonroot ownership; distroless's default home is
# read-only, so the SQLite file needs an explicitly writable dir.
COPY --from=builder --chown=nonroot:nonroot /tmp /data
VOLUME ["/data"]

EXPOSE 8080
ENV PORT=8080 \
    DB_PATH=/data/pack-calculator.db

USER nonroot:nonroot
ENTRYPOINT ["/app/pack-calculator"]
