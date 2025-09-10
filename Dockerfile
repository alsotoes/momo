FROM golang:1.20-alpine AS build
WORKDIR /app

# Cache deps
COPY go.mod ./
COPY go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/momo src/momo.go

FROM alpine:3.19
WORKDIR /app

# Copy binary and config
COPY --from=build /out/momo /app/momo
COPY src/conf /app/conf

# Utilities for healthchecks and debugging
RUN apk add --no-cache netcat-openbsd

# Default entrypoint; pass args via docker run/compose command
ENTRYPOINT ["/app/momo"]
