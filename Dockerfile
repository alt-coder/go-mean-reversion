# Use the official Go image as base
FROM golang:1.21-alpine AS builder

# Set working directory
WORKDIR /app

# Install git (needed for go mod download)
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o trading_bot .

# Use a minimal base image for the final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

# Set timezone to Asia/Kolkata for IST
ENV TZ=Asia/Kolkata

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/trading_bot .

# Create a non-root user for security
RUN adduser -D -s /bin/sh appuser
USER appuser

# Expose port (if needed for health checks)
EXPOSE 8080

# Run the binary
CMD ["./trading_bot"]