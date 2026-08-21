package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/rendau/loom/server/internal/config"
)

// AdminHttpServerCreate — HTTP-сервер SPA админки на отдельном порту:
// статика собранного Nuxt (ADMIN_DIR), рантайм-конфиг /config.js из env
// (window.__APP_CONFIG__ — значения задаются после билда SPA, один билд на
// все окружения) и SPA-fallback на index.html для клиентского роутинга.
// Возвращает nil, если каталога статики нет — админка выключена.
func AdminHttpServerCreate() *http.Server {
	dir := config.Conf.AdminDir
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}

	return &http.Server{
		Addr:              ":" + config.Conf.AdminPort,
		Handler:           adminHandler(dir),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       time.Minute,
		MaxHeaderBytes:    300 * 1024,
	}
}

func adminHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.URL.Path == "/config.js" {
			serveAdminConfig(w)
			return
		}

		// хэшированные ассеты Nuxt можно кэшировать навечно
		if strings.HasPrefix(r.URL.Path, "/_nuxt/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
			return
		}

		// существующий файл — отдаём как есть, иначе SPA-fallback на
		// index.html (клиентский роутер разрулит путь сам)
		path := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

// serveAdminConfig отдаёт рантайм-конфиг SPA. Значения экранируются через
// json.Marshal — env не может сломать или подменить скрипт.
func serveAdminConfig(w http.ResponseWriter) {
	apiBaseUrl, _ := json.Marshal(config.Conf.AdminApiBaseUrl)

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("window.__APP_CONFIG__ = { apiBaseUrl: " + string(apiBaseUrl) + " };\n"))
}
