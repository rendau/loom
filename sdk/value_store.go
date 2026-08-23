package loom

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/rendau/loom/api/server_v1"
)

// valueStore — порт мелких значений тасков (аналог XCom): key-value через
// control plane в распределённом режиме, файлы рана — в локальном.
type valueStore interface {
	Push(ctx context.Context, runID, task string, attempt int, key string, value []byte) error
	Pull(ctx context.Context, runID, task, key string) ([]byte, error)
}

// ── локальный режим: файлы рана ─────────────────────────

// fsValueStore хранит значения файлами <dir>/<run>/values/<task>/<key>.json —
// остаются после рана для изучения, как и артефакты.
type fsValueStore struct {
	dir string
}

func (s *fsValueStore) path(runID, task, key string) string {
	return filepath.Join(s.dir, runID, "values", task, key+".json")
}

func (s *fsValueStore) Push(_ context.Context, runID, task string, _ int, key string, value []byte) error {
	path := s.path(runID, task, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	// атомарная запись: читатель не должен увидеть недописанный файл
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, value, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *fsValueStore) Pull(_ context.Context, runID, task, key string) ([]byte, error) {
	raw, err := os.ReadFile(s.path(runID, task, key))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("value %q of task %q not found", key, task)
	}
	return raw, err
}

// ── распределённый режим: TaskValueService control plane ─

type grpcValueStore struct {
	conn   *grpc.ClientConn
	client pb.TaskValueServiceClient
}

func dialGrpcValueStore(addr string) (*grpcValueStore, error) {
	conn, err := grpc.NewClient(addr, dialOpts()...)
	if err != nil {
		return nil, fmt.Errorf("dial control plane %q: %w", addr, err)
	}
	return &grpcValueStore{conn: conn, client: pb.NewTaskValueServiceClient(conn)}, nil
}

func (s *grpcValueStore) Close() error {
	return s.conn.Close()
}

func (s *grpcValueStore) Push(ctx context.Context, runID, task string, attempt int, key string, value []byte) error {
	var v structpb.Value
	if err := v.UnmarshalJSON(value); err != nil {
		return fmt.Errorf("invalid value json: %w", err)
	}

	_, err := s.client.PushTaskValue(ctx, &pb.TaskValuePushReq{
		RunId:   runID,
		Task:    task,
		Attempt: int32(attempt),
		Key:     key,
		Value:   &v,
	})
	return err
}

func (s *grpcValueStore) Pull(ctx context.Context, runID, task, key string) ([]byte, error) {
	rep, err := s.client.PullTaskValue(ctx, &pb.TaskValuePullReq{
		RunId: runID,
		Task:  task,
		Key:   key,
	})
	if err != nil {
		return nil, err
	}
	return rep.GetValue().MarshalJSON()
}
