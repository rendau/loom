FROM alpine:latest

RUN apk add --no-cache --upgrade ca-certificates tzdata curl docker-cli

WORKDIR /app

COPY ./cmd/build/. ./
COPY ./migrations ./migrations
# статика SPA админки: перед сборкой образа выполнить `make build-admin`
COPY ./admin-ui ./admin-ui

CMD ["./svc"]
