package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/rs/cors"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	commonPb "github.com/rendau/loom/api/common"
	"github.com/rendau/loom/server/internal/config"
	"github.com/rendau/loom/server/internal/errs"
)

func GrpcGatewayCreateHandler(muxHook func(*runtime.ServeMux) error) (http.Handler, error) {
	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
		runtime.WithErrorHandler(gatewayErrorHandler),
	)

	if muxHook != nil {
		if err := muxHook(mux); err != nil {
			return nil, fmt.Errorf("grpc-gateway: muxHook: %w", err)
		}
	}

	handler := http.Handler(mux)

	// cors middleware
	if config.Conf.HttpCors {
		handler = cors.New(cors.Options{
			AllowOriginFunc: func(origin string) bool { return true },
			AllowedMethods: []string{
				http.MethodGet,
				http.MethodPut,
				http.MethodPost,
				http.MethodDelete,
			},
			AllowedHeaders: []string{
				"Accept",
				"Content-Type",
				"X-Requested-With",
				"Authorization",
			},
			AllowCredentials: true,
			MaxAge:           604800,
		}).Handler(handler)
	}

	// recover middleware
	handler = func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					slog.Error(
						"Recovered from panic",
						slog.Any("error", err),
						slog.Any("recovery_stacktrace", string(debug.Stack())),
					)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			h.ServeHTTP(w, r)
		})
	}(handler)

	return handler, nil
}

// gatewayErrorHandler отдаёт доменные ошибки телом common.ErrorRep: детали
// grpc-статуса, если handler их приложил, иначе — код/сообщение статуса.
func gatewayErrorHandler(_ context.Context, _ *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, _ *http.Request, err error) {
	st := status.Convert(err)

	rep := &commonPb.ErrorRep{
		Code:    errs.ServiceNA.Error(),
		Message: st.Message(),
	}
	for _, detail := range st.Details() {
		if d, ok := detail.(*commonPb.ErrorRep); ok {
			rep = d
			break
		}
	}

	body, marshalErr := marshaler.Marshal(rep)
	if marshalErr != nil {
		slog.Error("GRPC_GW: ErrorHandler: failed to marshal", "error", marshalErr)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(runtime.HTTPStatusFromCode(st.Code()))
	if _, writeErr := w.Write(body); writeErr != nil {
		slog.Error("GRPC_GW: ErrorHandler: failed to write response", "error", writeErr)
	}
}
