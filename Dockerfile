# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS builder
WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api ./cmd/api

FROM alpine:3.20
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/api /app/api
COPY migrations /app/migrations
COPY docs /app/docs

EXPOSE 8080

CMD ["/app/api"]

