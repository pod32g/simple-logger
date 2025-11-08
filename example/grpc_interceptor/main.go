package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	log "github.com/pod32g/simple-logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
)

// jsonCodec allows us to send JSON payloads without generated protobuf code.
type jsonCodec struct{}

func (jsonCodec) Name() string { return "json" }

func (jsonCodec) Marshal(v interface{}) ([]byte, error) { return json.Marshal(v) }

func (jsonCodec) Unmarshal(data []byte, v interface{}) error { return json.Unmarshal(data, v) }

// Register the codec on init so both server and client use it.
func init() {
	encoding.RegisterCodec(jsonCodec{})
}

// HelloRequest represents a minimal gRPC request without protobuf generation.
type HelloRequest struct {
	Name string `json:"name"`
}

// HelloReply is the response sent back to the client.
type HelloReply struct {
	Message string `json:"message"`
}

// GreeterService implements a simple SayHello method.
type GreeterService struct {
	logger *log.Logger
}

func (s *GreeterService) SayHello(ctx context.Context, req *HelloRequest) (*HelloReply, error) {
	s.logger.Info("received hello", log.String("name", req.Name))
	return &HelloReply{Message: "hello " + req.Name}, nil
}

func greeterUnaryHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HelloRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(*GreeterService).SayHello(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/example.Greeter/SayHello",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(*GreeterService).SayHello(ctx, req.(*HelloRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var greeterServiceDesc = grpc.ServiceDesc{
	ServiceName: "example.Greeter",
	HandlerType: (*GreeterService)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "SayHello",
			Handler:    greeterUnaryHandler,
		},
	},
}

func loggingInterceptor(logger *log.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		fields := []log.Field{
			log.String("method", info.FullMethod),
			log.Any("duration", duration),
		}
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if reqID := md.Get("x-request-id"); len(reqID) > 0 {
				fields = append(fields, log.String("request_id", reqID[0]))
			}
		}

		if err != nil {
			logger.ErrorFields("grpc call failed", append(fields, log.Error("error", err))...)
			return resp, err
		}

		logger.InfoFields("grpc call completed", fields...)
		return resp, nil
	}
}

func main() {
	logger := log.ApplyConfig(log.DefaultConfig())
	logger.SetFormatter(&log.JSONFormatter{})
	defer logger.Close()

	service := &GreeterService{logger: logger}

	server := grpc.NewServer(grpc.ChainUnaryInterceptor(loggingInterceptor(logger)))
	server.RegisterService(&greeterServiceDesc, service)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		logger.FatalFields("failed to listen", log.Error("error", err))
	}

	go func() {
		logger.Info("starting gRPC server", log.String("addr", lis.Addr().String()))
		if err := server.Serve(lis); err != nil {
			logger.Error("gRPC server stopped", log.Error("error", err))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(jsonCodec{})),
	)
	if err != nil {
		logger.FatalFields("dial failed", log.Error("error", err))
	}
	defer conn.Close()

	md := metadata.Pairs("x-request-id", "1234")
	callCtx := metadata.NewOutgoingContext(ctx, md)

	req := &HelloRequest{Name: "world"}
	var reply HelloReply
	if err := conn.Invoke(callCtx, "/example.Greeter/SayHello", req, &reply); err != nil {
		logger.Error("client call failed", log.Error("error", err))
	} else {
		fmt.Println("client received:", reply.Message)
	}

	server.GracefulStop()
}
