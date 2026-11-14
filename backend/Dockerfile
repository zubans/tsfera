FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /tsfera-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Europe/Moscow
WORKDIR /app
COPY --from=builder /tsfera-server /usr/local/bin/tsfera-server
EXPOSE 8080
CMD ["tsfera-server"]
