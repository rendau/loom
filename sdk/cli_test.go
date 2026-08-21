package loom

import (
	"bytes"
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/rendau/loom/api/server_v1"
)

// manifestSink — стаб control plane: принимает PushDagManifest describe-Job'а.
type manifestSink struct {
	pb.UnimplementedDagServiceServer

	ch chan *pb.DagPushManifestReq
}

func (s *manifestSink) PushDagManifest(_ context.Context, req *pb.DagPushManifestReq) (*emptypb.Empty, error) {
	s.ch <- req
	return &emptypb.Empty{}, nil
}

func startManifestSink(t *testing.T) (*manifestSink, string) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	sink := &manifestSink{ch: make(chan *pb.DagPushManifestReq, 1)}
	srv := grpc.NewServer()
	pb.RegisterDagServiceServer(srv, sink)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return sink, lis.Addr().String()
}

func TestDescribeStdout(t *testing.T) {
	t.Setenv(EnvDescribeID, "")

	d := New("demo")
	d.Task("a", nopTask)

	var stdout, stderr bytes.Buffer
	code := runCLI(d, []string{"describe"}, &stdout, &stderr)

	require.Equal(t, 0, code, stderr.String())
	assert.Contains(t, stdout.String(), `"name": "demo"`)
}

func TestDescribePush(t *testing.T) {
	sink, addr := startManifestSink(t)
	t.Setenv(EnvServerAddr, addr)
	t.Setenv(EnvDescribeID, "test-describe-id")

	d := New("demo")
	d.Task("a", nopTask)

	var stdout, stderr bytes.Buffer
	code := runCLI(d, []string{"describe"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())

	req := <-sink.ch
	assert.Equal(t, "test-describe-id", req.GetDescribeId())
	assert.Empty(t, req.GetError())
	assert.Contains(t, string(req.GetManifest()), `"name": "demo"`)
	// печать в stdout сохраняется (диагностика через kubectl logs)
	assert.Contains(t, stdout.String(), `"name": "demo"`)
}

func TestDescribePushValidationError(t *testing.T) {
	sink, addr := startManifestSink(t)
	t.Setenv(EnvServerAddr, addr)
	t.Setenv(EnvDescribeID, "test-describe-id")

	d := New("Bad Name")

	var stdout, stderr bytes.Buffer
	code := runCLI(d, []string{"describe"}, &stdout, &stderr)
	require.Equal(t, 2, code)

	// ошибка валидации уходит на server, чтобы регистрация не ждала таймаута
	req := <-sink.ch
	assert.Equal(t, "test-describe-id", req.GetDescribeId())
	assert.Contains(t, req.GetError(), "invalid dag name")
	assert.Empty(t, req.GetManifest())
}

func TestDescribePushWithoutServerAddr(t *testing.T) {
	t.Setenv(EnvDescribeID, "test-describe-id")
	t.Setenv(EnvServerAddr, "")

	d := New("demo")
	d.Task("a", nopTask)

	var stdout, stderr bytes.Buffer
	code := runCLI(d, []string{"describe"}, &stdout, &stderr)

	require.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), EnvServerAddr)
}
