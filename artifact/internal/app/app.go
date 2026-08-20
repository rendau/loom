package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	pb "github.com/rendau/loom/api/artifact_v1"
	"github.com/rendau/loom/artifact/internal/config"
	domainArtifact "github.com/rendau/loom/artifact/internal/domain/artifact"
	grpcHandler "github.com/rendau/loom/artifact/internal/handler/grpc"
)

type App struct {
	grpcServer       *GrpcServer
	systemHttpServer *http.Server

	exitCode int
}

func (a *App) Init() {
	// logger
	initLogger(config.Conf.Debug, config.Conf.LogLevel)

	// domain
	artifactSvc, err := domainArtifact.New(config.Conf.DataDir)
	errCheck(err, "artifact service init")
	slog.Info("artifact data dir: " + config.Conf.DataDir)

	// grpc server
	{
		artifactHandler := grpcHandler.NewArtifact(artifactSvc)

		a.grpcServer = NewGrpcServer("main", func(server *grpc.Server) {
			pb.RegisterArtifactServiceServer(server, artifactHandler)
		})
	}

	// system http server (healthcheck)
	{
		a.systemHttpServer = SystemHttpServerCreate()
	}
}

func (a *App) PreStartHook() {
	slog.Info("PreStartHook")
}

func (a *App) Start() {
	slog.Info("Starting")

	// grpc server
	{
		err := a.grpcServer.Start()
		errCheck(err, "grpcServer.Start")
	}

	// system http server
	{
		go func() {
			err := a.systemHttpServer.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("system-http-server stopped", "error", err)
			}
		}()
		slog.Info("system-http-server started " + a.systemHttpServer.Addr)
	}
}

func (a *App) Listen() {
	signalCtx, signalCtxCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer signalCtxCancel()

	// wait signal
	<-signalCtx.Done()
}

func (a *App) Stop() {
	slog.Info("Shutting down...")

	// system http server
	{
		ctx, ctxCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ctxCancel()

		if err := a.systemHttpServer.Shutdown(ctx); err != nil {
			slog.Error("system-http-server shutdown error", "error", err)
			a.exitCode = 1
		}
	}

	// grpc server
	a.grpcServer.Stop()
}

func (a *App) WaitJobs() {
	slog.Info("waiting jobs")
}

func (a *App) Exit() {
	slog.Info("Exit")

	os.Exit(a.exitCode)
}

func errCheck(err error, msg string) {
	if err != nil {
		if msg != "" {
			err = fmt.Errorf("%s: %w", msg, err)
		}
		slog.Error(err.Error())
		os.Exit(1)
	}
}
