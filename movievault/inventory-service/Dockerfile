# Stage 1: Build the Go binary
FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o inventory-service .
 
# Stage 2: Minimal production image (~10MB instead of ~800MB)
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/inventory-service .
EXPOSE 8080
CMD ["./inventory-service"]

