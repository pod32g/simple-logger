package grpclog_test

import (
	"context"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
	"github.com/pod32g/simple-logger/bridge/grpclog"
	"github.com/pod32g/simple-logger/logtest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryInterceptorLogsSuccess(t *testing.T) {
	obs, logger := logtest.New(log.DEBUG)
	interceptor := grpclog.UnaryServerInterceptor(logger)

	_, err := interceptor(context.Background(), "req",
		&grpc.UnaryServerInfo{FullMethod: "/svc/Method"},
		func(context.Context, interface{}) (interface{}, error) { return "resp", nil })
	if err != nil {
		t.Fatal(err)
	}

	entry := obs.AssertLogged(t, "grpc call")
	if entry.Level != log.INFO {
		t.Errorf("level = %v, want INFO", entry.Level)
	}
	if got, _ := entry.Field("grpc.method"); got != "/svc/Method" {
		t.Errorf("method = %v", got)
	}
	if got, _ := entry.Field("grpc.code"); got != "OK" {
		t.Errorf("code = %v, want OK", got)
	}
}

func TestUnaryInterceptorLogsFailure(t *testing.T) {
	obs, logger := logtest.New(log.DEBUG)
	interceptor := grpclog.UnaryServerInterceptor(logger)

	wantErr := status.Error(codes.NotFound, "missing")
	_, err := interceptor(context.Background(), "req",
		&grpc.UnaryServerInfo{FullMethod: "/svc/Missing"},
		func(context.Context, interface{}) (interface{}, error) { return nil, wantErr })
	if err == nil {
		t.Fatal("expected the handler error to propagate")
	}

	entry := obs.AssertLogged(t, "grpc call")
	if entry.Level != log.ERROR {
		t.Errorf("level = %v, want ERROR", entry.Level)
	}
	if got, _ := entry.Field("grpc.code"); got != "NotFound" {
		t.Errorf("code = %v, want NotFound", got)
	}
	if got, _ := entry.Field("error"); !strings.Contains(got.(string), "missing") {
		t.Errorf("error field = %v", got)
	}
}

// A request ID in metadata reaches the handler's context.
func TestRequestIDPropagates(t *testing.T) {
	_, logger := logtest.New(log.DEBUG)
	interceptor := grpclog.UnaryServerInterceptor(logger)

	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("x-request-id", "req-99"))

	var seen string
	_, err := interceptor(ctx, "req", &grpc.UnaryServerInfo{FullMethod: "/svc/M"},
		func(inner context.Context, _ interface{}) (interface{}, error) {
			seen = log.RequestID(inner)
			return nil, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if seen != "req-99" {
		t.Errorf("handler saw request id %q, want req-99", seen)
	}
}

func TestLevelOptions(t *testing.T) {
	obs, logger := logtest.New(log.DEBUG)
	interceptor := grpclog.UnaryServerInterceptor(logger,
		grpclog.WithLevel(log.DEBUG),
		grpclog.WithErrorLevel(log.WARN))

	_, _ = interceptor(context.Background(), "r", &grpc.UnaryServerInfo{FullMethod: "/a/B"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	_, _ = interceptor(context.Background(), "r", &grpc.UnaryServerInfo{FullMethod: "/a/B"},
		func(context.Context, interface{}) (interface{}, error) {
			return nil, status.Error(codes.Internal, "boom")
		})

	if got := obs.FilterLevel(log.DEBUG).Len(); got != 1 {
		t.Errorf("expected one DEBUG entry, got %d", got)
	}
	if got := obs.FilterLevel(log.WARN).Len(); got != 1 {
		t.Errorf("expected one WARN entry, got %d", got)
	}
}
