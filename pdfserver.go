package main

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// pdfURLPath is where the preview fetches the compiled PDF.
	pdfURLPath = "/pdf/main.pdf"
	// projectURLPrefix serves files out of the open project, so figures can be
	// displayed without copying their bytes across the bridge.
	projectURLPrefix = "/project/"
)

// projectFileURL is the URL for a project-relative file. The revision defeats
// the webview cache, which otherwise keeps showing a figure that has been
// replaced on disk.
func projectFileURL(rel string, revision int) string {
	return projectURLPrefix + strings.TrimPrefix(rel, "/") + "?rev=" + strconv.Itoa(revision)
}

// assetMiddleware routes our own URLs before anything else looks at them.
//
// This has to be middleware rather than the asset server's Handler option.
// Handler is a *fallback*, consulted only for requests the asset chain declines
// — and under `wails dev` the chain never declines anything: it proxies to the
// Vite dev server, whose SPA fallback answers unknown paths with index.html and
// HTTP 200. The preview was fed HTML and PDF.js reported "Invalid PDF
// structure". Middleware wraps the whole chain, so dev and production match.
func (a *App) assetMiddleware() func(http.Handler) http.Handler {
	pdf := a.pdfHandler()
	files := a.projectFileHandler()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == pdfURLPath:
				pdf.ServeHTTP(w, r)
			case strings.HasPrefix(r.URL.Path, projectURLPrefix):
				files.ServeHTTP(w, r)
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

// pdfHandler serves the most recent compiled PDF.
//
// This replaces Phase 0's base64-over-the-bridge shortcut: a real document is
// megabytes, and encoding it into a JSON string on every save would be wasteful
// on both sides.
func (a *App) pdfHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pdfURLPath {
			http.NotFound(w, r)
			return
		}

		a.mu.Lock()
		path := a.pdfPath
		a.mu.Unlock()

		if path == "" {
			// Nothing compiled yet — normal on first launch, not an error.
			http.NotFound(w, r)
			return
		}
		serveWholeFile(w, r, path, "application/pdf")
	})
}

// projectFileHandler serves a file from the open project, read-only.
func (a *App) projectFileHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, projectURLPrefix)
		if rel == "" {
			http.NotFound(w, r)
			return
		}

		a.mu.Lock()
		proj := a.proj
		a.mu.Unlock()

		if proj == nil {
			http.NotFound(w, r)
			return
		}
		// Abs is what keeps a crafted URL from reading outside the project.
		abs, err := proj.Abs(rel)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		contentType := mime.TypeByExtension(filepath.Ext(abs))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		serveWholeFile(w, r, abs, contentType)
	})
}

// serveWholeFile writes a file in one complete response.
//
// Deliberately not http.ServeFile: that negotiates Range and conditional
// requests, and the webview's custom-scheme transport does not deal with the
// resulting 206/304 responses — the preview's fetch simply never settled,
// leaving the pane stuck on its placeholder with no error to show. The files
// here are small enough that one plain response is the better trade.
func serveWholeFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "reading file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	// The revision query already busts the cache; no-store keeps a stale file
	// from surviving a revision rollback (a newly opened project, say).
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	// A failed write means the webview went away; nothing useful to do.
	_, _ = w.Write(data)
}
