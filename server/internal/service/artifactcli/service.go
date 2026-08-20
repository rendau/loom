// Package artifactcli — gRPC-клиент artifact-сервера для control plane:
// страховочный FinishAttempt при завершении/смерти попытки и удаление
// артефактов рана (retention).
package artifactcli

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/rendau/loom/api/artifact_v1"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

type Service struct {
	conn   *grpc.ClientConn
	client pb.ArtifactServiceClient
}

func New(addr string) (*Service, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient: %w", err)
	}

	return &Service{conn: conn, client: pb.NewArtifactServiceClient(conn)}, nil
}

func (s *Service) FinishAttempt(ctx context.Context, ref runModel.AttemptRef) error {
	_, err := s.client.FinishAttempt(ctx, &pb.FinishAttemptRequest{
		RunId:   ref.RunId,
		Task:    ref.Task,
		Attempt: ref.Attempt,
	})
	if err != nil {
		return fmt.Errorf("FinishAttempt: %w", err)
	}
	return nil
}

func (s *Service) DeleteRunArtifacts(ctx context.Context, runId string) error {
	_, err := s.client.DeleteRunArtifacts(ctx, &pb.DeleteRunArtifactsRequest{RunId: runId})
	if err != nil {
		return fmt.Errorf("DeleteRunArtifacts: %w", err)
	}
	return nil
}

func (s *Service) Close() error {
	return s.conn.Close()
}
