package handler

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/rendau/loom/api/artifact_v1"
	domain "github.com/rendau/loom/artifact/internal/domain/artifact"
)

const readChunkSize = 256 * 1024

type Artifact struct {
	pb.UnsafeArtifactServiceServer

	svc  *domain.Service
	auth *authorizer
}

// NewArtifact создаёт handler; непустой authSecret включает проверку
// attempt-токенов (metadata loom-token).
func NewArtifact(svc *domain.Service, authSecret string) *Artifact {
	return &Artifact{svc: svc, auth: newAuthorizer(authSecret)}
}

func (h *Artifact) WriteArtifact(stream pb.ArtifactService_WriteArtifactServer) error {
	first, err := stream.Recv()
	if err != nil {
		return encodeErr(err)
	}

	header := first.GetHeader()
	if header == nil {
		return status.Error(codes.InvalidArgument, "first message must be header")
	}
	if err = h.auth.checkAttempt(stream.Context(), header.GetRunId(), header.GetTask(), header.GetAttempt()); err != nil {
		return err
	}

	w, err := h.svc.BeginWrite(decodeRef(header))
	if err != nil {
		return encodeErr(err)
	}
	defer func() { _ = w.Abort() }() // no-op после commit

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			// клиент закрыл стрим без commit — писатель упал: abort
			return status.Error(codes.Aborted, "stream closed without commit")
		}
		if err != nil {
			return encodeErr(err)
		}

		switch m := msg.GetMsg().(type) {
		case *pb.WriteArtifactRequest_Chunk:
			if _, err = w.Write(m.Chunk); err != nil {
				return encodeErr(err)
			}
		case *pb.WriteArtifactRequest_Commit:
			if !m.Commit {
				return status.Error(codes.InvalidArgument, "commit must be true")
			}
			size, err := w.Commit()
			if err != nil {
				return encodeErr(err)
			}
			return stream.SendAndClose(&pb.WriteArtifactResponse{Size: size})
		default:
			return status.Error(codes.InvalidArgument, "unexpected message")
		}
	}
}

func (h *Artifact) ReadArtifact(req *pb.ReadArtifactRequest, stream pb.ArtifactService_ReadArtifactServer) error {
	if err := h.auth.checkRun(stream.Context(), req.GetRef().GetRunId()); err != nil {
		return err
	}

	r, err := h.svc.OpenRead(stream.Context(), decodeRef(req.GetRef()), req.GetOffset(), req.GetFollow())
	if err != nil {
		return encodeErr(err)
	}
	defer func() { _ = r.Close() }()

	buf := make([]byte, readChunkSize)
	for {
		n, err := r.Next(stream.Context(), buf)
		if n > 0 {
			if sendErr := stream.Send(&pb.ReadArtifactResponse{Chunk: buf[:n]}); sendErr != nil {
				return sendErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return encodeErr(err)
		}
	}
}

func (h *Artifact) StatArtifact(ctx context.Context, req *pb.StatArtifactRequest) (*pb.StatArtifactResponse, error) {
	if err := h.auth.checkRun(ctx, req.GetRef().GetRunId()); err != nil {
		return nil, err
	}

	state, size, err := h.svc.Stat(decodeRef(req.GetRef()))
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.StatArtifactResponse{State: encodeState(state), Size: size}, nil
}

func (h *Artifact) AbortArtifact(ctx context.Context, req *pb.AbortArtifactRequest) (*pb.AbortArtifactResponse, error) {
	ref := req.GetRef()
	if err := h.auth.checkAttempt(ctx, ref.GetRunId(), ref.GetTask(), ref.GetAttempt()); err != nil {
		return nil, err
	}

	if err := h.svc.AbortRef(decodeRef(ref)); err != nil {
		return nil, encodeErr(err)
	}

	return &pb.AbortArtifactResponse{}, nil
}

func (h *Artifact) FinishAttempt(ctx context.Context, req *pb.FinishAttemptRequest) (*pb.FinishAttemptResponse, error) {
	if err := h.auth.checkAttempt(ctx, req.GetRunId(), req.GetTask(), req.GetAttempt()); err != nil {
		return nil, err
	}

	key := domain.AttemptKey{RunID: req.GetRunId(), Task: req.GetTask(), Attempt: req.GetAttempt()}
	if err := h.svc.FinishAttempt(key); err != nil {
		return nil, encodeErr(err)
	}

	return &pb.FinishAttemptResponse{}, nil
}

func (h *Artifact) DeleteRunArtifacts(ctx context.Context, req *pb.DeleteRunArtifactsRequest) (*pb.DeleteRunArtifactsResponse, error) {
	if err := h.auth.checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.svc.DeleteRun(req.GetRunId()); err != nil {
		return nil, encodeErr(err)
	}

	return &pb.DeleteRunArtifactsResponse{}, nil
}

func decodeRef(v *pb.ArtifactRef) domain.Ref {
	if v == nil {
		return domain.Ref{}
	}

	return domain.Ref{RunID: v.RunId, Task: v.Task, Attempt: v.Attempt, Name: v.Name}
}

func encodeState(v domain.State) pb.ArtifactState {
	switch v {
	case domain.StateWriting:
		return pb.ArtifactState_ARTIFACT_STATE_WRITING
	case domain.StateCommitted:
		return pb.ArtifactState_ARTIFACT_STATE_COMMITTED
	case domain.StateAborted:
		return pb.ArtifactState_ARTIFACT_STATE_ABORTED
	default:
		return pb.ArtifactState_ARTIFACT_STATE_UNSPECIFIED
	}
}

// encodeErr маппит доменные ошибки в grpc-статусы.
func encodeErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrInvalidRef):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, domain.ErrAborted):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, domain.ErrNotWriting), errors.Is(err, domain.ErrAttemptFinished):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		if _, ok := status.FromError(err); ok {
			return err
		}
		return status.Error(codes.Internal, err.Error())
	}
}
