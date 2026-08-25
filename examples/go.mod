module github.com/rendau/loom/examples

go 1.27

replace github.com/rendau/loom/sdk => ../sdk

require github.com/rendau/loom/sdk v0.0.0-00010101000000-000000000000

require (
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/rendau/loom/api v0.1.0 // indirect
	github.com/samber/lo v1.53.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/grpc v1.83.1 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)
