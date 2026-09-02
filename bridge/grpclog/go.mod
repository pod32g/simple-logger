// The gRPC bridge is its own module so that gRPC stays out of the dependency
// graph of everyone who only wants a logger.
module github.com/pod32g/simple-logger/bridge/grpclog

go 1.25.0

require (
	github.com/pod32g/simple-logger v0.8.2
	google.golang.org/grpc v1.83.1
)

require (
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)

replace github.com/pod32g/simple-logger => ../..
