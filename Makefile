.DEFAULT_GOAL := build

.SILENT:

build: build-artifact build-server

build-artifact:
	cd artifact && mkdir -p cmd/build && CGO_ENABLED=0 go build -o cmd/build/svc cmd/main.go

build-server:
	cd server && mkdir -p cmd/build && CGO_ENABLED=0 go build -o cmd/build/svc cmd/main.go

# SPA админки: статика кладётся в server/admin-ui (дефолт ADMIN_DIR сервера;
# server/Dockerfile копирует её в образ)
build-admin:
	cd admin && pnpm install && pnpm generate
	rm -rf server/admin-ui && cp -R admin/.output/public server/admin-ui

clean:
	rm -rf artifact/cmd/build server/cmd/build server/admin-ui admin/.output

# интеграционные тесты server/test требуют Postgres: TEST_PG_DSN=postgres://...
test:
	cd sdk && go test ./...
	cd artifact && go test ./...
	cd server && go test ./...

# Установка плагинов protoc
proto-plugins:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest

generate-proto:
	protoc -I api/proto -I api/vendor-proto \
	--go_out api --go_opt paths=source_relative \
	--go-grpc_out api --go-grpc_opt paths=source_relative \
	api/proto/common/*.proto api/proto/artifact_v1/*.proto
	protoc -I api/proto -I api/vendor-proto \
	--go_out api --go_opt paths=source_relative \
	--go-grpc_out api --go-grpc_opt paths=source_relative \
	--grpc-gateway_out api --grpc-gateway_opt paths=source_relative \
	api/proto/server_v1/*.proto
