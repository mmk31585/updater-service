FROM golang:1.27-alpine AS builder

WORKDIR /app
ENV GOPROXY=https://package-mirror.liara.ir/repository/go/                       
ENV GOSUMDB=off
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/entry ./cmd/entry

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/entry /app/entry
COPY --from=builder /app/migrations /app/migrations
ENV MIGRATIONS_DIR=/app/migrations

CMD ["./entry"]