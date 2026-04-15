# ---- Build stage ----
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Download deps first (layer cache)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN go build -o bin/renyra ./cmd/api

# ---- Run stage ----
FROM alpine:3.20

WORKDIR /app

# ca-certificates is required for outbound TLS (MongoDB Atlas, Firebase, etc.)
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/bin/renyra ./bin/renyra

EXPOSE 8080

CMD ["./bin/renyra"]
