package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/rendau/loom/api/artifact_v1"
	"github.com/rendau/loom/api/attempttoken"
	"github.com/rendau/loom/sdk/streamstore"
)

// writeChunkSize — максимальный размер chunk-сообщения записи. Каждый Write
// уходит на сервер сразу (лишь режется на chunk'и) — без клиентского
// буфера: follow-читатели видят данные по мере записи, как в локальном
// режиме. Кому важна пропускная способность на мелких записях — оборачивает
// выход в bufio.Writer.
const writeChunkSize = 256 * 1024

// tokenMetadataKey — ключ metadata с attempt-токеном; общий контракт с
// artifact-сервером и лог-приёмником (api/attempttoken).
const tokenMetadataKey = attempttoken.MetadataKey

// grpcStore — remote-реализация artifactStore: gRPC-клиент artifact-сервера.
// Стейт-машина стримов живёт на сервере (общий sdk/streamstore), поэтому
// семантика обмена совпадает с локальным режимом вплоть до кода.
type grpcStore struct {
	conn   *grpc.ClientConn
	client pb.ArtifactServiceClient
	token  string
}

func dialGrpcStore(addr, token string) (*grpcStore, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial artifact server %q: %w", addr, err)
	}

	return &grpcStore{conn: conn, client: pb.NewArtifactServiceClient(conn), token: token}, nil
}

func (s *grpcStore) Close() error {
	return s.conn.Close()
}

// outCtx прикладывает attempt-токен к исходящему вызову.
func (s *grpcStore) outCtx(ctx context.Context) context.Context {
	if s.token == "" {
		return ctx
	}

	return metadata.AppendToOutgoingContext(ctx, tokenMetadataKey, s.token)
}

func (s *grpcStore) OpenWrite(ctx context.Context, ref ArtifactRef) (ArtifactWriter, error) {
	stream, err := s.client.WriteArtifact(s.outCtx(ctx))
	if err != nil {
		return nil, decodeGrpcErr(err)
	}

	w := &grpcWriter{stream: stream}
	if err = stream.Send(&pb.WriteArtifactRequest{Msg: &pb.WriteArtifactRequest_Header{Header: encodeGrpcRef(ref)}}); err != nil {
		return nil, w.recvErr()
	}

	return w, nil
}

// OpenRead открывает follow-чтение с offset 0. grpc не подтверждает
// открытие отдельным сообщением, поэтому ошибки открытия (NOT_FOUND,
// ABORTED) приходят статусом стрима — из первого Read, а не отсюда.
func (s *grpcStore) OpenRead(ctx context.Context, ref ArtifactRef) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(s.outCtx(ctx))

	stream, err := s.client.ReadArtifact(ctx, &pb.ReadArtifactRequest{Ref: encodeGrpcRef(ref), Offset: 0, Follow: true})
	if err != nil {
		cancel()
		return nil, decodeGrpcErr(err)
	}

	return &grpcReader{stream: stream, cancel: cancel}, nil
}

// finishAttempt помечает попытку завершённой на artifact-сервере. Вызов
// идемпотентен: control plane повторит его как страховку при смерти пода.
func (s *grpcStore) finishAttempt(ctx context.Context, runID, task string, attempt int) error {
	_, err := s.client.FinishAttempt(s.outCtx(ctx), &pb.FinishAttemptRequest{RunId: runID, Task: task, Attempt: int32(attempt)})
	return decodeGrpcErr(err)
}

func encodeGrpcRef(v ArtifactRef) *pb.ArtifactRef {
	return &pb.ArtifactRef{RunId: v.RunID, Task: v.Task, Attempt: int32(v.Attempt), Name: v.Name}
}

// grpcWriter пишет артефакт клиентским стримом: header уже отправлен,
// Commit шлёт commit-маркер, Abort просто закрывает стрим — сервер трактует
// закрытие без commit как abort записи.
type grpcWriter struct {
	stream grpc.ClientStreamingClient[pb.WriteArtifactRequest, pb.WriteArtifactResponse]
}

func (w *grpcWriter) Write(p []byte) (int, error) {
	for off := 0; off < len(p); off += writeChunkSize {
		chunk := p[off:min(off+writeChunkSize, len(p))]
		if err := w.stream.Send(&pb.WriteArtifactRequest{Msg: &pb.WriteArtifactRequest_Chunk{Chunk: chunk}}); err != nil {
			return 0, w.recvErr()
		}
	}

	return len(p), nil
}

func (w *grpcWriter) Commit() error {
	if err := w.stream.Send(&pb.WriteArtifactRequest{Msg: &pb.WriteArtifactRequest_Commit{Commit: true}}); err != nil {
		return w.recvErr()
	}
	if _, err := w.stream.CloseAndRecv(); err != nil {
		return decodeGrpcErr(err)
	}

	return nil
}

func (w *grpcWriter) Abort() error {
	_, err := w.stream.CloseAndRecv()
	if err == nil || status.Code(err) == codes.Aborted {
		return nil
	}

	return decodeGrpcErr(err)
}

// recvErr достаёт настоящий статус стрима: неудачный Send возвращает
// io.EOF, причина доступна только через CloseAndRecv.
func (w *grpcWriter) recvErr() error {
	_, err := w.stream.CloseAndRecv()
	if err == nil {
		err = errors.New("write stream closed unexpectedly")
	}

	return decodeGrpcErr(err)
}

// grpcReader адаптирует серверный стрим чтения к io.ReadCloser.
type grpcReader struct {
	stream grpc.ServerStreamingClient[pb.ReadArtifactResponse]
	cancel context.CancelFunc
	buf    []byte // недочитанный хвост текущего chunk'а
	err    error
}

func (r *grpcReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.err != nil {
			return 0, r.err
		}

		msg, err := r.stream.Recv()
		if err != nil {
			r.err = decodeGrpcErr(err) // io.EOF остаётся io.EOF: артефакт дочитан
			return 0, r.err
		}
		r.buf = msg.GetChunk()
	}

	n := copy(p, r.buf)
	r.buf = r.buf[n:]

	return n, nil
}

func (r *grpcReader) Close() error {
	r.cancel()
	return nil
}

// decodeGrpcErr маппит статусы artifact-сервера обратно в ошибки
// streamstore: код и поведение remote-стора совпадают с локальным.
// FAILED_PRECONDITION делится по тексту — сервер кладёт в message
// sentinel-ошибку домена.
func decodeGrpcErr(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}

	st, ok := status.FromError(err)
	if !ok {
		return err
	}

	switch st.Code() {
	case codes.NotFound:
		return fmt.Errorf("%w: %s", streamstore.ErrNotFound, st.Message())
	case codes.AlreadyExists:
		return fmt.Errorf("%w: %s", streamstore.ErrAlreadyExists, st.Message())
	case codes.Aborted:
		return fmt.Errorf("%w: %s", streamstore.ErrAborted, st.Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", streamstore.ErrInvalidRef, st.Message())
	case codes.FailedPrecondition:
		if strings.Contains(st.Message(), streamstore.ErrAttemptFinished.Error()) {
			return fmt.Errorf("%w: %s", streamstore.ErrAttemptFinished, st.Message())
		}
		return fmt.Errorf("%w: %s", streamstore.ErrNotWriting, st.Message())
	default:
		return err
	}
}
