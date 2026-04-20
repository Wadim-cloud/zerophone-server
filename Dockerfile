# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies including ZeroMQ
RUN apk add --no-cache gcc musl-dev libzmq-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o zerophone .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite libzmq

WORKDIR /app/

COPY --from=builder /app/zerophone .
COPY --from=builder /app/static ./static

# Expose HTTP and ZeroMQ ports
EXOSE 8080 5555 5556 5557 5558

ENV ZEROPHONE_CLUSTER=1 \
    ZEROPHONE_PORT=8081

CMD ["./zerophone", "--addr", ":8080"]