package webdav

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	xwebdav "golang.org/x/net/webdav"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/torrent"
)

// Mount registers read-only WebDAV at /webdav/ when enabled in config.
func Mount(r chi.Router, cfg config.Config, manager *torrent.Manager) error {
	user, pass, ok := cfg.WebDAVAuth()
	if !ok {
		return nil
	}

	handler := &xwebdav.Handler{
		Prefix:     "/webdav",
		FileSystem: newTorrentFS(manager),
		LockSystem: xwebdav.NewMemLS(),
	}

	mounted := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !readOnlyMethod(req.Method) {
			w.Header().Set("Allow", "OPTIONS, GET, HEAD, PROPFIND")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler.ServeHTTP(w, req)
	})

	auth := basicAuth(user, pass, mounted)
	r.Handle("/webdav/*", auth)
	r.Get("/webdav", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/webdav/", http.StatusPermanentRedirect)
	})
	return nil
}

func readOnlyMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, "PROPFIND":
		return true
	default:
		return false
	}
}

func basicAuth(user, pass string, next http.Handler) http.Handler {
	userBytes := []byte(user)
	passBytes := []byte(pass)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(u), userBytes) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), passBytes) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="DebridNest WebDAV"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Enabled reports whether WebDAV credentials are configured for mounting.
func Enabled(cfg config.Config) bool {
	_, _, ok := cfg.WebDAVAuth()
	return ok
}

// MountPath returns the public WebDAV URL path prefix.
func MountPath(cfg config.Config) string {
	if !Enabled(cfg) {
		return ""
	}
	return strings.TrimRight(cfg.PublicURL, "/") + "/webdav/"
}
