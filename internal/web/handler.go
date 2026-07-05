package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist/*
var dist embed.FS

// Handler returns an http.Handler that serves the built frontend.
// It falls back to index.html for SPA routing.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	return spaHandler{root: http.FileServer(http.FS(sub))}
}

type spaHandler struct {
	root http.Handler
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	_, err := dist.ReadFile(path.Join("dist", p))
	if err == nil {
		http.ServeFileFS(w, r, dist, path.Join("dist", p))
		return
	}
	index, err := dist.ReadFile("dist/index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(index)
}
