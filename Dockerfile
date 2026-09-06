# Build the wallet-service binary.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /wallet-service ./cmd/wallet-service

# Minimal runtime image.
FROM alpine:3.20
RUN adduser -D -u 10001 wallet
USER wallet
COPY --from=build /wallet-service /usr/local/bin/wallet-service
ENTRYPOINT ["wallet-service"]
