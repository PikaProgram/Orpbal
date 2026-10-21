FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20 AS app

WORKDIR /app

RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 app

COPY --from=builder /out/server /app/server

ENV HOST=0.0.0.0 \
    PORT=8080

USER app

EXPOSE 8080

CMD ["/app/server"]
