FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src

COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://proxy.golang.org,direct
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /app/server .

FROM alpine:latest
RUN apk add --no-cache ca-certificates

RUN addgroup -S app && adduser -S -G app app
WORKDIR /app

COPY --from=builder /app/server /app/server
COPY --from=builder /src/frontend /app/frontend

RUN chown -R app:app /app
USER app

EXPOSE 8080
ENTRYPOINT ["/app/server"]