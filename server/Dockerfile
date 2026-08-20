FROM alpine:latest

RUN apk add --no-cache --upgrade ca-certificates tzdata curl docker-cli

WORKDIR /app

COPY ./cmd/build/. ./
COPY ./migrations ./migrations

CMD ["./svc"]
