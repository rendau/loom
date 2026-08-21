// Package artifactcli — gRPC-клиент artifact-сервера для control plane:
// страховочный FinishAttempt при завершении/смерти попытки и удаление
// артефактов рана (retention). Вызовы подписываются короткоживущим
// admin-токеном (общий секрет AUTH_SECRET); без секрета — без токена.
package artifactcli

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/rendau/loom/api/artifact_v1"
	"github.com/rendau/loom/api/attempttoken"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

// adminTokenTTL — срок действия подписи одного вызова.
const adminTokenTTL = time.Minute

type Service struct {
	conn   *grpc.ClientConn
	client pb.ArtifactServiceClient
	secret []byte
}

func New(addr, authSecret string) (*Service, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient: %w", err)
	}

	s := &Service{conn: conn, client: pb.NewArtifactServiceClient(conn)}
	if authSecret != "" {
		s.secret = []byte(authSecret)
	}
	return s, nil
}

// authCtx подписывает вызов admin-токеном.
func (s *Service) authCtx(ctx context.Context) (context.Context, error) {
	if s.secret == nil {
		return ctx, nil
	}

	token, err := attempttoken.Sign(s.secret, attempttoken.Claims{
		Admin:     true,
		ExpiresAt: time.Now().Add(adminTokenTTL).Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("sign admin token: %w", err)
	}

	return metadata.AppendToOutgoingContext(ctx, attempttoken.MetadataKey, token), nil
}

func (s *Service) FinishAttempt(ctx context.Context, ref runModel.AttemptRef) error {
	ctx, err := s.authCtx(ctx)
	if err != nil {
		return err
	}

	_, err = s.client.FinishAttempt(ctx, &pb.FinishAttemptRequest{
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
	ctx, err := s.authCtx(ctx)
	if err != nil {
		return err
	}

	_, err = s.client.DeleteRunArtifacts(ctx, &pb.DeleteRunArtifactsRequest{RunId: runId})
	if err != nil {
		return fmt.Errorf("DeleteRunArtifacts: %w", err)
	}
	return nil
}

func (s *Service) Close() error {
	return s.conn.Close()
}
