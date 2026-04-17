# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o zerophone .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite
WORKDIR /root/

COPY --from=builder /app/zerophone .
COPY --from=builder /app/static ./static

EXPOSE 8080

CMD ["./zerophone", "--db", "/root/zerophone.db"]
