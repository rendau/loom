.DEFAULT_GOAL := build

.SILENT:

build: build-artifact

build-artifact:
	cd artifact && mkdir -p cmd/build && CGO_ENABLED=0 go build -o cmd/build/svc cmd/main.go

clean:
	rm -rf artifact/cmd/build

test:
	cd sdk && go test ./...
	cd artifact && go test ./...

lint:
	cd sdk && golangci-lint run
	cd artifact && golangci-lint run

# Установка плагинов protoc
proto-plugins:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

generate-proto:
	protoc -I api/proto \
	--go_out api --go_opt paths=source_relative \
	--go-grpc_out api --go-grpc_opt paths=source_relative \
	api/proto/artifact_v1/*.proto
