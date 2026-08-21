module github.com/rendau/loom/artifact

go 1.27

replace (
	github.com/rendau/loom/api => ../api
	github.com/rendau/loom/sdk => ../sdk
)

require (
	github.com/caarlos0/env/v9 v9.0.0
	github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus v1.1.0
	github.com/joho/godotenv v1.5.1
	github.com/prometheus/client_golang v1.24.1
	github.com/rendau/loom/api v0.0.0-00010101000000-000000000000
	github.com/rendau/loom/sdk v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.12.1
	golang.org/x/sync v0.22.0
	google.golang.org/grpc v1.83.1
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.1.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/samber/lo v1.53.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)
