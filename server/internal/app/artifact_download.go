package app

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	artifactModel "github.com/rendau/loom/server/internal/domain/artifact/model"
	"github.com/rendau/loom/server/internal/errs"
	artifactUsc "github.com/rendau/loom/server/internal/usecase/artifact"
)

// ArtifactDownloadHandler — скачивание/превью содержимого артефакта:
//
//	GET /run/{run_id}/artifact/{task}/{attempt}/{name}/download
//	  ?limit_bytes=N — превью первых N байт (inline); без — attachment
//	  ?token=...     — сессия для прямой ссылки браузера (заголовок
//	                   Authorization на <a href> не поставить)
//
// Стримовый эндпоинт живёт рядом с grpc-gateway (обёрткой над его mux —
// CORS/recover применяются снаружи как обычно): gateway буферизует ответы
// unary-методов, а артефакты бывают большими.
func ArtifactDownloadHandler(auth AuthenticatorI, uc *artifactUsc.Usecase, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ref, ok := parseArtifactDownloadPath(r.URL.Path)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		token := httpBearerToken(r)
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token == "" {
			writeHTTPError(w, http.StatusUnauthorized, errs.NotAuthorized, "требуется вход")
			return
		}
		info, err := auth.Authenticate(r.Context(), token)
		if err != nil {
			writeHTTPError(w, http.StatusUnauthorized, errs.NotAuthorized, "сессия недействительна")
			return
		}
		_ = info // читать артефакты может любой аутентифицированный (как логи)

		var limit int64
		if raw := r.URL.Query().Get("limit_bytes"); raw != "" {
			limit, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || limit < 1 {
				writeHTTPError(w, http.StatusBadRequest, errs.InvalidRequest, "некорректный limit_bytes")
				return
			}
		}

		if limit > 0 {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition",
				`attachment; filename="`+ref.RunId+"_"+ref.Task+"_"+strconv.Itoa(int(ref.Attempt))+"_"+ref.Name+`"`)
		}

		cw := &countingWriter{w: w}
		if err = uc.Read(r.Context(), ref, limit, cw); err != nil {
			if cw.written == 0 {
				// до первого байта статус ещё не ушёл — честный код ошибки
				w.Header().Del("Content-Disposition")
				switch {
				case errors.Is(err, errs.ArtifactNotFound):
					writeHTTPError(w, http.StatusNotFound, errs.ArtifactNotFound, "артефакт не найден")
				case errors.Is(err, errs.ArtifactAborted):
					writeHTTPError(w, http.StatusConflict, errs.ArtifactAborted, "запись артефакта была прервана")
				default:
					slog.Error("artifact download", "error", err)
					writeHTTPError(w, http.StatusInternalServerError, errs.ServiceNA, "ошибка чтения артефакта")
				}
				return
			}
			// стрим уже шёл — остаётся только оборвать соединение
			slog.Warn("artifact download interrupted", "run_id", ref.RunId, "task", ref.Task, "error", err)
		}
	})
}

// parseArtifactDownloadPath разбирает
// /run/{run_id}/artifact/{task}/{attempt}/{name}/download.
func parseArtifactDownloadPath(path string) (artifactModel.Ref, bool) {
	parts := strings.Split(path, "/")
	// ["", "run", run_id, "artifact", task, attempt, name, "download"]
	if len(parts) != 8 || parts[1] != "run" || parts[3] != "artifact" || parts[7] != "download" {
		return artifactModel.Ref{}, false
	}
	attempt, err := strconv.Atoi(parts[5])
	if err != nil || attempt < 1 {
		return artifactModel.Ref{}, false
	}
	return artifactModel.Ref{
		RunId:   parts[2],
		Task:    parts[4],
		Attempt: int32(attempt),
		Name:    parts[6],
	}, true
}

func httpBearerToken(r *http.Request) string {
	const prefix = "bearer "
	v := r.Header.Get("Authorization")
	if len(v) > len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
		return v[len(prefix):]
	}
	return ""
}

func writeHTTPError(w http.ResponseWriter, status int, code errs.Err, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    code.Error(),
		"message": message,
	})
}

type countingWriter struct {
	w       http.ResponseWriter
	written int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.written += int64(n)
	return n, err
}
