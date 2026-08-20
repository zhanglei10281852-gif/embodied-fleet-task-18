# Runtime image for the embodied-robot-fleet backend (multi-stage, not latest).
FROM golang:1.26 AS backend
ENV GOTOOLCHAIN=local
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/scheduler ./cmd/scheduler \
 && go build -o /out/executor ./cmd/executor \
 && go build -o /out/migrate ./cmd/migrate

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=backend /out/scheduler /out/executor /out/migrate /app/bin/
COPY migrations ./migrations
ENV ROBOTFLEET_DATA_DIR=/app/data
ENV PATH=/app/bin:$PATH
EXPOSE 58552
CMD ["scheduler"]
