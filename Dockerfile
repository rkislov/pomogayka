FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/pomogayka ./cmd/server

FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata \
  && adduser -D -H -u 10001 appuser \
  && mkdir -p /app/data \
  && chown -R appuser:appuser /app
COPY --from=build /out/pomogayka /app/pomogayka
USER appuser
ENV ADDR=:8080
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["/app/pomogayka"]
