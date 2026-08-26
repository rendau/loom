package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/rendau/loom/api/server_v1"
)

// manifestSink — стаб control plane: принимает PushDagCatalog describe-Job'а.
type manifestSink struct {
	pb.UnimplementedProjectServiceServer

	ch chan *pb.DagPushCatalogReq
}

func (s *manifestSink) PushDagCatalog(_ context.Context, req *pb.DagPushCatalogReq) (*emptypb.Empty, error) {
	s.ch <- req
	return &emptypb.Empty{}, nil
}

func startManifestSink(t *testing.T) (*manifestSink, string) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	sink := &manifestSink{ch: make(chan *pb.DagPushCatalogReq, 1)}
	srv := grpc.NewServer()
	pb.RegisterProjectServiceServer(srv, sink)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return sink, lis.Addr().String()
}

func TestDescribeStdout(t *testing.T) {
	t.Setenv(EnvDescribeID, "")

	d := New("demo")
	d.Task("a", nopTask)

	var stdout, stderr bytes.Buffer
	code := runCLI([]*DAG{d}, []string{"describe"}, &stdout, &stderr)

	require.Equal(t, 0, code, stderr.String())

	var catalog Catalog
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &catalog))
	assert.Equal(t, Version, catalog.SDKVersion)
	require.Len(t, catalog.Dags, 1)
	assert.Equal(t, "demo", catalog.Dags[0].Name)
	require.NotNil(t, catalog.Dags[0].Manifest)
	assert.Empty(t, catalog.Dags[0].Error)
}

func TestDescribePush(t *testing.T) {
	sink, addr := startManifestSink(t)
	t.Setenv(EnvServerAddr, addr)
	t.Setenv(EnvDescribeID, "test-describe-id")

	d := New("demo")
	d.Task("a", nopTask)

	var stdout, stderr bytes.Buffer
	code := runCLI([]*DAG{d}, []string{"describe"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())

	req := <-sink.ch
	assert.Equal(t, "test-describe-id", req.GetDescribeId())
	assert.Empty(t, req.GetError())
	assert.Contains(t, string(req.GetCatalog()), `"name": "demo"`)
	assert.Contains(t, string(req.GetCatalog()), `"sdk_version"`)
	// печать в stdout сохраняется (диагностика через kubectl logs)
	assert.Contains(t, stdout.String(), `"name": "demo"`)
}

func TestDescribePushValidationError(t *testing.T) {
	sink, addr := startManifestSink(t)
	t.Setenv(EnvServerAddr, addr)
	t.Setenv(EnvDescribeID, "test-describe-id")

	d := New("Bad Name")

	var stdout, stderr bytes.Buffer
	code := runCLI([]*DAG{d}, []string{"describe"}, &stdout, &stderr)
	require.Equal(t, 2, code)

	// ошибка валидации уходит на server, чтобы регистрация не ждала таймаута;
	// валидных дагов в образе не осталось — сводка едет отдельным полем
	req := <-sink.ch
	assert.Equal(t, "test-describe-id", req.GetDescribeId())
	assert.Contains(t, req.GetError(), "invalid dag name")

	var catalog Catalog
	require.NoError(t, json.Unmarshal(req.GetCatalog(), &catalog))
	require.Len(t, catalog.Dags, 1)
	assert.Nil(t, catalog.Dags[0].Manifest)
	assert.Contains(t, catalog.Dags[0].Error, "invalid dag name")
}

func TestDescribePushWithoutServerAddr(t *testing.T) {
	t.Setenv(EnvDescribeID, "test-describe-id")
	t.Setenv(EnvServerAddr, "")

	d := New("demo")
	d.Task("a", nopTask)

	var stdout, stderr bytes.Buffer
	code := runCLI([]*DAG{d}, []string{"describe"}, &stdout, &stderr)

	require.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), EnvServerAddr)
}

// образ несёт несколько дагов: каталог перечисляет их все, порядок —
// как в вызове Main
func TestDescribeCatalogManyDags(t *testing.T) {
	t.Setenv(EnvDescribeID, "")

	etl := New("etl")
	etl.Task("a", nopTask)
	sync := New("nsi_sync")
	sync.Task("b", nopTask)

	var stdout, stderr bytes.Buffer
	code := runCLI([]*DAG{etl, sync}, []string{"describe"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())

	var catalog Catalog
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &catalog))
	require.Len(t, catalog.Dags, 2)
	assert.Equal(t, "etl", catalog.Dags[0].Name)
	assert.Equal(t, "nsi_sync", catalog.Dags[1].Name)
	require.NotNil(t, catalog.Dags[1].Manifest)
	assert.Equal(t, "nsi_sync", catalog.Dags[1].Manifest.Name)
}

// сломанный даг не отменяет регистрацию остальных: его ошибка едет в
// каталоге, код возврата остаётся успешным
func TestDescribeCatalogPartialError(t *testing.T) {
	sink, addr := startManifestSink(t)
	t.Setenv(EnvServerAddr, addr)
	t.Setenv(EnvDescribeID, "test-describe-id")

	good := New("etl")
	good.Task("a", nopTask)
	bad := New("nsi_sync") // без тасков даг валиден — ломаем зависимостью на чужой таск
	other := New("other")
	bad.Task("b", nopTask, After(other.Task("c", nopTask)))

	var stdout, stderr bytes.Buffer
	code := runCLI([]*DAG{good, bad}, []string{"describe"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())

	req := <-sink.ch
	assert.Empty(t, req.GetError()) // валидные даги есть — регистрация не падает

	var catalog Catalog
	require.NoError(t, json.Unmarshal(req.GetCatalog(), &catalog))
	require.Len(t, catalog.Dags, 2)
	assert.NotNil(t, catalog.Dags[0].Manifest)
	assert.Nil(t, catalog.Dags[1].Manifest)
	assert.Contains(t, catalog.Dags[1].Error, "belongs to another dag")
	assert.Contains(t, stderr.String(), `invalid dag "nsi_sync"`)
}

// дубль имени — ошибка уровня каталога: регистрировать такой образ нельзя
func TestDescribeCatalogDuplicateNames(t *testing.T) {
	sink, addr := startManifestSink(t)
	t.Setenv(EnvServerAddr, addr)
	t.Setenv(EnvDescribeID, "test-describe-id")

	first := New("etl")
	first.Task("a", nopTask)
	second := New("etl")
	second.Task("b", nopTask)

	var stdout, stderr bytes.Buffer
	code := runCLI([]*DAG{first, second}, []string{"describe"}, &stdout, &stderr)
	require.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "duplicate dag name")

	req := <-sink.ch
	assert.Contains(t, req.GetError(), "duplicate dag name")
	assert.Empty(t, req.GetCatalog())
}

func TestDescribeCatalogNoDags(t *testing.T) {
	t.Setenv(EnvDescribeID, "")

	var stdout, stderr bytes.Buffer
	code := runCLI(nil, []string{"describe"}, &stdout, &stderr)

	require.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "no dags declared")
}

func TestSelectDag(t *testing.T) {
	etl := New("etl")
	sync := New("nsi_sync")

	t.Run("single dag without name", func(t *testing.T) {
		d, err := selectDag([]*DAG{etl}, "")
		require.NoError(t, err)
		assert.Same(t, etl, d)
	})

	t.Run("many dags without name", func(t *testing.T) {
		_, err := selectDag([]*DAG{etl, sync}, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dag name is required")
		assert.Contains(t, err.Error(), "nsi_sync")
	})

	t.Run("by name", func(t *testing.T) {
		d, err := selectDag([]*DAG{etl, sync}, "nsi_sync")
		require.NoError(t, err)
		assert.Same(t, sync, d)
	})

	t.Run("unknown name", func(t *testing.T) {
		_, err := selectDag([]*DAG{etl, sync}, "missing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown dag "missing"`)
	})
}

// имя дага executor передаёт через env; флаг --dag приоритетнее
func TestRunDagFromEnv(t *testing.T) {
	t.Setenv(EnvDag, "missing")

	etl := New("etl")
	etl.Task("a", nopTask)

	var stdout, stderr bytes.Buffer
	code := runCLI([]*DAG{etl}, []string{"run"}, &stdout, &stderr)
	require.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), `unknown dag "missing"`)

	// флаг приоритетнее env; локальный ран пишет артефакты в CWD
	t.Chdir(t.TempDir())
	stderr.Reset()
	code = runCLI([]*DAG{etl}, []string{"run", "--dag=etl"}, &stdout, &stderr)
	assert.Equal(t, 0, code, stderr.String())
}
