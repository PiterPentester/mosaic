# Multi-stage build for minimal image
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o mosaic .

FROM scratch
WORKDIR /app
COPY --from=builder /app/mosaic .
EXPOSE 8080
CMD ["./mosaic"]
