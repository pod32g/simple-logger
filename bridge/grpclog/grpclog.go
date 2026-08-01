// Package grpclog provides gRPC interceptors that log each call through
// simple-logger, the way bridge/httplog does for net/http.
//
// It is a module of its own so that importing simple-logger does not pull gRPC
// into your dependency graph. Use it with:
//
//	go get github.com/pod32g/simple-logger/bridge/grpclog
package grpclog

import (
	"context"
	"time"

	log "github.com/pod32g/simple-logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Option configures the interceptors.
type Option func(*options)

type options struct {
	logger        *log.Logger
	requestHeader string
	level         log.LogLevel
	errorLevel    log.LogLevel
	payloadSize   bool
}

// WithLevel sets the level used for calls that succeed. Defaults to INFO.
func WithLevel(level log.LogLevel) Option {
	return func(o *options) { o.level = level }
}

// WithErrorLevel sets the level used for calls that fail. Defaults to ERROR.
func WithErrorLevel(level log.LogLevel) Option {
	return func(o *options) { o.errorLevel = level }
}

// WithRequestIDHeader reads a correlation ID from the given metadata key and
// puts it on the context, so handlers logging with *Context calls carry it.
// Defaults to "x-request-id".
func WithRequestIDHeader(key string) Option {
	return func(o *options) { o.requestHeader = key }
}

func newOptions(logger *log.Logger, opts []Option) *options {
	o := &options{
		logger:        logger,
		requestHeader: "x-request-id",
		level:         log.INFO,
		errorLevel:    log.ERROR,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// UnaryServerInterceptor logs one entry per unary call: method, status code,
// duration, and the error when there is one.
func UnaryServerInterceptor(logger *log.Logger, opts ...Option) grpc.UnaryServerInterceptor {
	o := newOptions(logger, opts)
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx = o.withRequestID(ctx)
		start := time.Now()

		resp, err := handler(ctx, req)

		o.log(ctx, info.FullMethod, start, err)
		return resp, err
	}
}

// StreamServerInterceptor logs one entry per stream, when the stream ends.
func StreamServerInterceptor(logger *log.Logger, opts ...Option) grpc.StreamServerInterceptor {
	o := newOptions(logger, opts)
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := o.withRequestID(ss.Context())
		start := time.Now()

		err := handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})

		o.log(ctx, info.FullMethod, start, err)
		return err
	}
}

// UnaryClientInterceptor logs each outgoing unary call.
func UnaryClientInterceptor(logger *log.Logger, opts ...Option) grpc.UnaryClientInterceptor {
	o := newOptions(logger, opts)
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, callOpts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, callOpts...)
		o.log(ctx, method, start, err)
		return err
	}
}

func (o *options) withRequestID(ctx context.Context) context.Context {
	if o.requestHeader == "" {
		return ctx
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	if values := md.Get(o.requestHeader); len(values) > 0 && values[0] != "" {
		return log.WithRequestID(ctx, values[0])
	}
	return ctx
}

func (o *options) log(ctx context.Context, method string, start time.Time, err error) {
	code := status.Code(err)
	fields := []log.Field{
		log.String("grpc.method", method),
		log.String("grpc.code", code.String()),
		log.Duration("grpc.duration", time.Since(start)),
	}

	level := o.level
	if err != nil && code != codes.OK {
		level = o.errorLevel
		fields = append(fields, log.Err("error", err))
	}
	o.logger.Log(level, "grpc call", append(fields, log.ContextFields(ctx)...))
}

// wrappedStream carries the request-ID-bearing context into the handler.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
