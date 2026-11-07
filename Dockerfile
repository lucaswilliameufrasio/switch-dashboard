# Stage 1: Build SvelteKit Switch Dashboard Frontend
FROM node:26-alpine AS frontend-builder
WORKDIR /build/frontend
COPY frontend/package.json ./
RUN npm install
COPY frontend/ ./
RUN mkdir -p /build/web/dist && npm run build

# Stage 2: Build Go Static Binary with Embedded SPA
FROM golang:1.27-alpine AS go-builder
WORKDIR /build
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/
COPY --from=frontend-builder /build/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /build/switch-dashboard ./cmd/server

# Stage 3: Hardened Non-Root Runtime Container
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 10001 -S switchos && \
    adduser -u 10001 -S -G switchos -h /app switchos

WORKDIR /app
COPY --from=go-builder /build/switch-dashboard /app/switch-dashboard
COPY device-templates/ /app/device-templates/
COPY device_types.yaml /app/device_types.yaml
COPY mac_vendors.txt /app/mac_vendors.txt

RUN mkdir -p /app/backup && chown -R switchos:switchos /app

USER switchos
EXPOSE 8080
ENV DASHBOARD_HOST=0.0.0.0
ENV DASHBOARD_PORT=8080
ENTRYPOINT ["/app/switch-dashboard"]
