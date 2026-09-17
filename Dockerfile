# --- Builder ---
FROM golang:1.25-alpine AS builder
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sonarqube-prometheus-exporter .

# --- Final ---
# alpine (not scratch) so the container has a shell + wget for
# docker-compose healthchecks, matching the other services in
# sonarqube-compose.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget && \
    adduser -D -H -u 10001 exporter
COPY --from=builder /out/sonarqube-prometheus-exporter /usr/local/bin/sonarqube-prometheus-exporter
USER exporter
EXPOSE 9091
ENTRYPOINT ["/usr/local/bin/sonarqube-prometheus-exporter"]
