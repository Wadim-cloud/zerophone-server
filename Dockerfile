# ZeroPhone Dockerfile
# Build: docker build -t zerophone .
# Run:   docker run -p 8080:8080 -p 5555:5555 -p 5556:5556 -p 5557:5557 -p 5558:5558 zerophone

FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev zeromq-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Copy certs for HTTPS
COPY cert.pem key.pem ./

RUN CGO_ENABLED=1 GOOS=linux go build -o zerophone .

FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite zeromq

WORKDIR /app

COPY --from=builder /app/zerophone .
COPY --from=builder /app/static ./static
COPY --from=builder /app/cluster ./cluster
COPY --from=builder /app/main.go ./
COPY --from=builder /app/cert.pem .
COPY --from=builder /app/key.pem .

EXPOSE 8080 5555 5556 5557 5558

ENV ZEROPHONE_CLUSTER=1

CMD ["./zerophone", "--addr", ":8080"]