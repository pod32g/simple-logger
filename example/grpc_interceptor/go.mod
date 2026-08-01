// The gRPC interceptor example lives in its own module so that
// google.golang.org/grpc -- which no package in the library itself imports --
// stays out of the dependency graph of everyone who imports simple-logger.
module github.com/pod32g/simple-logger/example/grpc_interceptor

go 1.25.0

require (
	github.com/pod32g/simple-logger v0.7.0
	google.golang.org/grpc v1.82.1
)

require (
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)

replace github.com/pod32g/simple-logger => ../..
