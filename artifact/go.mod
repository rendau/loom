module github.com/rendau/loom/artifact

go 1.27

replace (
	github.com/rendau/loom/api => ../api
	github.com/rendau/loom/sdk => ../sdk
)

require (
	github.com/caarlos0/env/v9 v9.0.0
	github.com/joho/godotenv v1.5.1
	github.com/rendau/loom/api v0.0.0-00010101000000-000000000000
	github.com/rendau/loom/sdk v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.12.1
	google.golang.org/grpc v1.83.1
)

require (
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/samber/lo v1.53.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.22.0
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)
