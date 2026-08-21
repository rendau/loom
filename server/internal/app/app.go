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

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mechta-market/mobone/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/rendau/loom/api/server_v1"
	"github.com/rendau/loom/server/internal/config"
	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	dagDb "github.com/rendau/loom/server/internal/domain/dag/repo/db"
	dagService "github.com/rendau/loom/server/internal/domain/dag/service"
	domainDagSync "github.com/rendau/loom/server/internal/domain/dagsync"
	poolDb "github.com/rendau/loom/server/internal/domain/pool/repo/db"
	poolService "github.com/rendau/loom/server/internal/domain/pool/service"
	domainRetention "github.com/rendau/loom/server/internal/domain/retention"
	runDb "github.com/rendau/loom/server/internal/domain/run/repo/db"
	runService "github.com/rendau/loom/server/internal/domain/run/service"
	domainScheduler "github.com/rendau/loom/server/internal/domain/scheduler"
	secretDb "github.com/rendau/loom/server/internal/domain/secret/repo/db"
	secretService "github.com/rendau/loom/server/internal/domain/secret/service"
	domainTasklog "github.com/rendau/loom/server/internal/domain/tasklog"
	grpcHandler "github.com/rendau/loom/server/internal/handler/grpc"
	"github.com/rendau/loom/server/internal/service/artifactcli"
	"github.com/rendau/loom/server/internal/service/dockercli"
	"github.com/rendau/loom/server/internal/service/dockerexecutor"
	"github.com/rendau/loom/server/internal/service/k8sclient"
	"github.com/rendau/loom/server/internal/service/k8sdescriber"
	"github.com/rendau/loom/server/internal/service/k8sexecutor"
	"github.com/rendau/loom/server/internal/service/registrycli"
	dagUsc "github.com/rendau/loom/server/internal/usecase/dag"
	poolUsc "github.com/rendau/loom/server/internal/usecase/pool"
	runUsc "github.com/rendau/loom/server/internal/usecase/run"
	secretUsc "github.com/rendau/loom/server/internal/usecase/secret"
	tasklogUsc "github.com/rendau/loom/server/internal/usecase/tasklog"
)

// executorI — исполняемая реализация executor-порта с жизненным циклом
// (k8s или docker).
type executorI interface {
	domainScheduler.ExecutorI
	Start() error
	Stop()
}

type App struct {
	pgpool      *pgxpool.Pool
	artifactCli *artifactcli.Service
	executor    executorI
	scheduler   *domainScheduler.Scheduler
	retention   *domainRetention.Service
	dagSync     *domainDagSync.Service

	grpcServer       *GrpcServer
	httpServer       *http.Server
	adminHttpServer  *http.Server
	systemHttpServer *http.Server

	exitCode int
}

func (a *App) Init() {
	var err error

	// logger
	initLogger(config.Conf.Debug, config.Conf.LogLevel)

	// pgpool
	a.pgpool, err = initPgPool(config.Conf.PgDsn)
	errCheck(err, "pgpool init")

	// migrations
	{
		runMigrations()
		slog.Info("PG-migrations have been successfully applied")
	}

	txm := mobone.NewTransactionManager(a.pgpool)
	repoBase := commonRepoPg.NewBase(a.pgpool, txm)

	// domain
	dagSvc := dagService.New(dagDb.New(repoBase))
	runSvc := runService.New(runDb.New(repoBase), txm)
	poolSvc := poolService.New(poolDb.New(repoBase))

	if config.Conf.SecretKey == "" {
		slog.Warn("secret encryption disabled (SECRET_KEY is empty)")
	}
	secretSvc, err := secretService.New(secretDb.New(repoBase), config.Conf.SecretKey)
	errCheck(err, "secret service init")

	tasklogSvc, err := domainTasklog.New(config.Conf.LogDir)
	errCheck(err, "tasklog service init")
	slog.Info("task log dir: " + config.Conf.LogDir)

	// services
	if config.Conf.AuthSecret == "" {
		slog.Warn("attempt token auth disabled (AUTH_SECRET is empty)")
	}
	a.artifactCli, err = artifactcli.New(config.Conf.ArtifactAddr, config.Conf.AuthSecret)
	errCheck(err, "artifact client init")

	// инспектор образов регистрации следует executor'у: в k8s — одноразовый
	// describe-Job, который push'ит манифест на control plane (решение №29),
	// иначе — docker-CLI (без sink'а)
	var (
		imageInspector dagUsc.ImageInspectorI = dockercli.New(config.Conf.DockerBin)
		manifestSink   dagUsc.ManifestSinkI
	)

	// executor + scheduler
	var schedulerNudger runUsc.SchedulerI = nopScheduler{}
	switch config.Conf.Executor {
	case "k8s":
		clientset, cErr := k8sclient.New(config.Conf.K8sKubeconfig)
		errCheck(cErr, "k8s client init")
		a.executor = k8sexecutor.New(clientset, config.Conf.K8sNamespace, config.Conf.K8sJobTTL,
			config.Conf.K8sImagePullSecret)

		describer := k8sdescriber.New(clientset, config.Conf.K8sNamespace,
			config.Conf.TaskServerAddr, config.Conf.K8sDescribeTimeout, config.Conf.K8sImagePullSecret)
		imageInspector = describer
		manifestSink = describer
	case "docker":
		a.executor = dockerexecutor.New(config.Conf.DockerBin, config.Conf.DockerNetwork, config.Conf.DockerPollTick)
	case "none":
		slog.Warn("executor disabled: runs will stay pending (EXECUTOR=none)")
	default:
		errCheck(fmt.Errorf("unknown executor %q", config.Conf.Executor), "executor init")
	}

	if a.executor != nil {
		a.scheduler = domainScheduler.New(
			runSvc, dagSvc, a.executor, a.artifactCli, tasklogSvc, secretSvc,
			domainScheduler.Config{
				Tick:          config.Conf.SchedTick,
				CronTick:      config.Conf.SchedCronTick,
				ReconcileTick: config.Conf.SchedReconcileTick,
				ZombieGrace:   config.Conf.SchedZombieGrace,
				ClaimLimit:    config.Conf.SchedClaimLimit,
				TaskEnv: domainScheduler.TaskEnv{
					ArtifactAddr: config.Conf.TaskArtifactAddr,
					ServerAddr:   config.Conf.TaskServerAddr,
				},
				TokenSecret: config.Conf.AuthSecret,
				TokenTTL:    config.Conf.TokenTTL,
			},
		)
		schedulerNudger = a.scheduler
	}

	// retention
	a.retention = domainRetention.New(runSvc, a.artifactCli, tasklogSvc,
		config.Conf.RunTTL, config.Conf.RetentionTick)

	// usecases
	dagUsecase := dagUsc.New(dagSvc, imageInspector, poolSvc, manifestSink)

	// авто-обновление дагов (решение №30): digest-чек registry + авто-
	// перерегистрация через обычный Register-флоу usecase'а
	a.dagSync = domainDagSync.New(dagSvc, registrycli.New(config.Conf.RegistryAuthFile),
		dagUsecase, config.Conf.DagSyncTick)

	runUsecase := runUsc.New(runSvc, dagSvc, schedulerNudger)
	tasklogUsecase := tasklogUsc.New(tasklogSvc, runSvc)
	poolUsecase := poolUsc.New(poolSvc)
	secretUsecase := secretUsc.New(secretSvc)

	// grpc server
	{
		dagHandler := grpcHandler.NewDag(dagUsecase)
		runHandler := grpcHandler.NewRun(runUsecase)
		tasklogHandler := grpcHandler.NewTaskLog(tasklogUsecase, config.Conf.AuthSecret)
		taskValueHandler := grpcHandler.NewTaskValue(runUsecase, config.Conf.AuthSecret)
		poolHandler := grpcHandler.NewPool(poolUsecase)
		secretHandler := grpcHandler.NewSecret(secretUsecase)

		a.grpcServer = NewGrpcServer("main", func(server *grpc.Server) {
			pb.RegisterDagServiceServer(server, dagHandler)
			pb.RegisterRunServiceServer(server, runHandler)
			pb.RegisterTaskLogServiceServer(server, tasklogHandler)
			pb.RegisterTaskValueServiceServer(server, taskValueHandler)
			pb.RegisterPoolServiceServer(server, poolHandler)
			pb.RegisterSecretServiceServer(server, secretHandler)
		})
	}

	// http-gw server
	{
		var handler http.Handler

		handler, err = GrpcGatewayCreateHandler(func(mux *runtime.ServeMux) error {
			opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

			conn, dialErr := grpc.NewClient("localhost:"+config.Conf.GrpcPort, opts...)
			if dialErr != nil {
				return fmt.Errorf("grpc.NewClient: %w", dialErr)
			}

			handlers := []func(context.Context, *runtime.ServeMux, *grpc.ClientConn) error{
				pb.RegisterDagServiceHandler,
				pb.RegisterRunServiceHandler,
				pb.RegisterTaskLogServiceHandler,
				pb.RegisterTaskValueServiceHandler,
				pb.RegisterPoolServiceHandler,
				pb.RegisterSecretServiceHandler,
			}
			for _, h := range handlers {
				if hErr := h(context.Background(), mux, conn); hErr != nil {
					return fmt.Errorf("grpc-gateway: register grpc-handler: %w", hErr)
				}
			}
			return nil
		})
		errCheck(err, "grpcGatewayCreateHandler")

		a.httpServer = &http.Server{
			Addr:              ":" + config.Conf.HttpPort,
			Handler:           handler,
			ReadHeaderTimeout: 2 * time.Second,
			ReadTimeout:       time.Minute,
			MaxHeaderBytes:    300 * 1024,
		}
	}

	// admin SPA server (отдельный порт; nil — статика не собрана)
	{
		a.adminHttpServer = AdminHttpServerCreate()
		if a.adminHttpServer == nil {
			slog.Warn("admin ui disabled: static dir not found", "dir", config.Conf.AdminDir)
		}
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

	// http-gw server
	{
		go func() {
			err := a.httpServer.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("http-server stopped", "error", err)
			}
		}()
		slog.Info("http-server started " + a.httpServer.Addr)
	}

	// admin SPA server
	if a.adminHttpServer != nil {
		go func() {
			err := a.adminHttpServer.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("admin-http-server stopped", "error", err)
			}
		}()
		slog.Info("admin-http-server started " + a.adminHttpServer.Addr)
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

	// executor + scheduler: сначала executor (события), затем планировщик
	if a.executor != nil {
		err := a.executor.Start()
		errCheck(err, "executor.Start")
		slog.Info("executor started: " + config.Conf.Executor)
	}
	if a.scheduler != nil {
		a.scheduler.Start()
		slog.Info("scheduler started")
	}

	a.retention.Start()
	a.dagSync.Start()
}

func (a *App) Listen() {
	signalCtx, signalCtxCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer signalCtxCancel()

	// wait signal
	<-signalCtx.Done()
}

func (a *App) Stop() {
	slog.Info("Shutting down...")

	// планировщик первым: перестать запускать и финализировать
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	if a.executor != nil {
		a.executor.Stop()
	}
	a.retention.Stop()
	a.dagSync.Stop()

	// http-gw server
	{
		ctx, ctxCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ctxCancel()

		if err := a.httpServer.Shutdown(ctx); err != nil {
			slog.Error("http-server shutdown error", "error", err)
			a.exitCode = 1
		}
	}

	// admin SPA server
	if a.adminHttpServer != nil {
		ctx, ctxCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ctxCancel()

		if err := a.adminHttpServer.Shutdown(ctx); err != nil {
			slog.Error("admin-http-server shutdown error", "error", err)
			a.exitCode = 1
		}
	}

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

	_ = a.artifactCli.Close()
	a.pgpool.Close()

	os.Exit(a.exitCode)
}

// nopScheduler — заглушка Nudge при выключенном executor'е (EXECUTOR=none).
type nopScheduler struct{}

func (nopScheduler) Nudge() {}

func errCheck(err error, msg string) {
	if err != nil {
		if msg != "" {
			err = fmt.Errorf("%s: %w", msg, err)
		}
		slog.Error(err.Error())
		os.Exit(1)
	}
}
