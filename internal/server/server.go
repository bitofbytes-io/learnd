package server

import (
	"net/http"

	"github.com/drywaters/learnd/internal/config"
	"github.com/drywaters/learnd/internal/handler"
	"github.com/drywaters/learnd/internal/middleware"
	"github.com/drywaters/learnd/internal/repository"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// Server represents the HTTP server
type Server struct {
	cfg              *config.Config
	entryRepo        *repository.EntryRepository
	summaryCacheRepo *repository.SummaryCacheRepository
}

// New creates a new Server
func New(cfg *config.Config, entryRepo *repository.EntryRepository, summaryCacheRepo *repository.SummaryCacheRepository) *Server {
	return &Server{
		cfg:              cfg,
		entryRepo:        entryRepo,
		summaryCacheRepo: summaryCacheRepo,
	}
}

// Router returns the configured chi router
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.Logger)
	r.Use(chimw.Recoverer)

	// Static files
	const staticCacheControl = "public, max-age=86400"
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", withCacheControl(staticCacheControl, http.StripPrefix("/static/", fileServer)))

	// Root-level static files (favicons, manifest, etc.)
	for _, file := range []string{
		"favicon.ico",
		"apple-touch-icon.png",
		"favicon-16x16.png",
		"favicon-32x32.png",
		"android-chrome-192x192.png",
		"android-chrome-512x512.png",
		"site.webmanifest",
	} {
		r.Get("/"+file, serveStaticFile("static/"+file))
	}

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Auth handlers
	authHandler := handler.NewAuthHandler(s.cfg.APIToken, s.cfg.SecureCookies)
	r.Get("/login", authHandler.LoginPage)
	r.With(middleware.SameOrigin).Post("/login", authHandler.Login)
	r.With(middleware.SameOrigin).Post("/logout", authHandler.Logout)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(s.cfg.APIToken, s.cfg.SecureCookies))

		// Capture handler
		captureHandler := handler.NewCaptureHandler(s.entryRepo)
		r.Get("/", captureHandler.CapturePage)

		// Entry API
		entryHandler := handler.NewEntryHandler(s.entryRepo)
		r.Post("/api/entries", entryHandler.Create)
		r.Get("/api/entries", entryHandler.List)
		r.Get("/api/entries/{id}", entryHandler.Get)
		r.Put("/api/entries/{id}", entryHandler.Update)
		r.Delete("/api/entries/{id}", entryHandler.Delete)
		r.Post("/api/entries/{id}/refresh-enrichment", entryHandler.RefreshEnrichment)
		r.Post("/api/entries/{id}/refresh-summary", entryHandler.RefreshSummary)
		r.Get("/entries/{id}/status", entryHandler.Status)
		r.Get("/entries/{id}/edit", entryHandler.EditPage)

		// Report handler
		reportHandler := handler.NewReportHandler(s.entryRepo)
		r.Get("/reports", reportHandler.ReportsPage)
		r.Get("/api/reports", reportHandler.GetReport)
		r.Get("/api/reports/export", reportHandler.ExportCSV)
	})

	return r
}

func serveStaticFile(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}

func withCacheControl(cacheControl string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheControl)
		next.ServeHTTP(w, r)
	})
}
