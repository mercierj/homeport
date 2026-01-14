package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/api/handlers"
	"github.com/homeport/homeport/internal/app/backup"
	"github.com/homeport/homeport/internal/app/cache"
	"github.com/homeport/homeport/internal/app/docker"
	"github.com/homeport/homeport/internal/app/identity"
	"github.com/homeport/homeport/internal/app/logs"
	appPolicy "github.com/homeport/homeport/internal/app/policy"
	"github.com/homeport/homeport/internal/app/providers"
	"github.com/homeport/homeport/internal/app/queues"
	"github.com/homeport/homeport/internal/app/secrets"
	"github.com/homeport/homeport/internal/app/stacks"
	"github.com/homeport/homeport/internal/pkg/logger"
)

type Config struct {
	Host    string
	Port    int
	NoAuth  bool
	Verbose bool
	Version string
}

type Server struct {
	config           Config
	router           *chi.Mux
	httpServer       *http.Server
	dockerService    *docker.Service
	dockerHandler    *handlers.DockerHandler
	metricsHandler   *handlers.MetricsHandler
	logsHandler      *handlers.LogsHandler
	identityService  *identity.Service
	identityHandler  *handlers.IdentityHandler
	functionsHandler *handlers.FunctionsHandler
	dnsHandler       *handlers.DNSHandler
	queuesHandler    *handlers.QueuesHandler
	cacheHandler     *handlers.CacheHandler
	secretsHandler   *handlers.SecretsHandler
	backupHandler    *handlers.BackupHandler
	stacksHandler    *handlers.StacksHandler
	terminalHandler  *handlers.TerminalHandler
	policyHandler     *handlers.PolicyHandler
	migrateHandler    *handlers.MigrateHandler
	deployHandler     *handlers.DeployHandler
	syncHandler       *handlers.SyncHandler
	cutoverHandler    *handlers.CutoverHandler
	providersHandler  *handlers.ProvidersHandler
}

func NewServer(cfg Config) (*Server, error) {
	s := &Server{config: cfg}

	// Initialize Identity service
	identitySvc := identity.NewService(nil)
	s.identityService = identitySvc
	s.identityHandler = handlers.NewIdentityHandler(identitySvc)

	// Initialize Functions handler
	functionsHandler, err := handlers.NewFunctionsHandler()
	if err != nil {
		logger.Warn("Functions handler not available", "error", err)
	} else {
		s.functionsHandler = functionsHandler
	}

	// Initialize DNS handler
	dnsHandler, err := handlers.NewDNSHandler()
	if err != nil {
		logger.Warn("DNS handler not available", "error", err)
	} else {
		s.dnsHandler = dnsHandler
	}

	// Initialize Queues handler (creates service internally)
	queuesHandler, err := handlers.NewQueuesHandler(queues.Config{})
	if err != nil {
		logger.Warn("Queues handler not available", "error", err)
	} else {
		s.queuesHandler = queuesHandler
	}

	// Initialize Cache handler (creates service internally)
	cacheHandler, err := handlers.NewCacheHandler(cache.Config{})
	if err != nil {
		logger.Warn("Cache handler not available", "error", err)
	} else {
		s.cacheHandler = cacheHandler
	}

	// Initialize Secrets handler (creates service internally)
	secretsHandler, err := handlers.NewSecretsHandler(secrets.Config{})
	if err != nil {
		logger.Warn("Secrets handler not available", "error", err)
	} else {
		s.secretsHandler = secretsHandler
	}

	// Initialize Backup handler
	backupHandler, err := handlers.NewBackupHandler(&backup.Config{})
	if err != nil {
		logger.Warn("Backup handler not available", "error", err)
	} else {
		s.backupHandler = backupHandler
	}

	// Initialize Stacks handler
	stacksHandler, err := handlers.NewStacksHandler(&stacks.Config{})
	if err != nil {
		logger.Warn("Stacks handler not available", "error", err)
	} else {
		s.stacksHandler = stacksHandler
	}

	// Initialize Policy handler
	policyHandler, err := handlers.NewPolicyHandler(&appPolicy.Config{})
	if err != nil {
		logger.Warn("Policy handler not available", "error", err)
	} else {
		s.policyHandler = policyHandler
	}

	// Initialize Migrate handler
	s.migrateHandler = handlers.NewMigrateHandler()

	// Initialize Deploy handler
	s.deployHandler = handlers.NewDeployHandler()

	// Initialize Sync handler
	s.syncHandler = handlers.NewSyncHandler()

	// Initialize Cutover handler
	s.cutoverHandler = handlers.NewCutoverHandler()

	// Initialize Providers handler
	providersSvc := providers.NewService()
	s.providersHandler = handlers.NewProvidersHandler(providersSvc)

	// Initialize Docker service
	dockerSvc, err := docker.NewService()
	if err != nil {
		logger.Warn("Docker not available", "error", err)
	} else {
		s.dockerService = dockerSvc
	}

	// Initialize handlers
	if s.dockerService != nil {
		dockerHandler, err := handlers.NewDockerHandler()
		if err != nil {
			logger.Warn("Docker handler not available", "error", err)
		} else {
			s.dockerHandler = dockerHandler
		}

		metricsHandler, err := handlers.NewMetricsHandler(s.dockerService)
		if err != nil {
			logger.Warn("Metrics handler not available", "error", err)
		} else {
			s.metricsHandler = metricsHandler
		}

		logsSvc, err := logs.NewService(s.dockerService)
		if err != nil {
			logger.Warn("Logs service not available", "error", err)
		} else {
			s.logsHandler = handlers.NewLogsHandler(logsSvc)
		}

		terminalHandler, err := handlers.NewTerminalHandler()
		if err != nil {
			logger.Warn("Terminal handler not available", "error", err)
		} else {
			s.terminalHandler = terminalHandler
		}
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	if s.config.Verbose {
		r.Use(middleware.Logger)
	}
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// CORS for frontend dev
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", s.handleHealth)

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			render.JSON(w, r, map[string]string{
				"status":  "ok",
				"version": "v1",
			})
		})

		// Identity routes
		if s.identityHandler != nil {
			s.identityHandler.RegisterRoutes(r)
		}

		// Functions routes
		if s.functionsHandler != nil {
			r.Route("/functions", func(r chi.Router) {
				r.Get("/", s.functionsHandler.HandleListFunctions)
				r.Post("/", s.functionsHandler.HandleCreateFunction)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.functionsHandler.HandleGetFunction)
					r.Put("/", s.functionsHandler.HandleUpdateFunction)
					r.Delete("/", s.functionsHandler.HandleDeleteFunction)
					r.Post("/invoke", s.functionsHandler.HandleInvokeFunction)
					r.Get("/logs", s.functionsHandler.HandleGetFunctionLogs)
				})
			})
		}

		// DNS routes
		if s.dnsHandler != nil {
			r.Route("/dns/zones", func(r chi.Router) {
				r.Get("/", s.dnsHandler.HandleListZones)
				r.Post("/", s.dnsHandler.HandleCreateZone)
				r.Route("/{zoneID}", func(r chi.Router) {
					r.Get("/", s.dnsHandler.HandleGetZone)
					r.Delete("/", s.dnsHandler.HandleDeleteZone)
					r.Post("/validate", s.dnsHandler.HandleValidateZone)
					r.Route("/records", func(r chi.Router) {
						r.Get("/", s.dnsHandler.HandleListRecords)
						r.Post("/", s.dnsHandler.HandleCreateRecord)
						r.Route("/{recordID}", func(r chi.Router) {
							r.Get("/", s.dnsHandler.HandleGetRecord)
							r.Put("/", s.dnsHandler.HandleUpdateRecord)
							r.Delete("/", s.dnsHandler.HandleDeleteRecord)
						})
					})
				})
			})
		}

		// Backup routes
		if s.backupHandler != nil {
			s.backupHandler.RegisterRoutes(r)
		}

		// Stacks routes
		if s.stacksHandler != nil {
			s.stacksHandler.RegisterRoutes(r)
		}

		// Terminal routes (WebSocket)
		if s.terminalHandler != nil {
			s.terminalHandler.RegisterRoutes(r)
		}

		// Policy routes
		if s.policyHandler != nil {
			s.policyHandler.RegisterRoutes(r)
		}

		// Migrate routes
		if s.migrateHandler != nil {
			r.Route("/migrate", func(r chi.Router) {
				r.Post("/analyze", s.migrateHandler.HandleAnalyze)
				r.Post("/discover", s.migrateHandler.HandleDiscover)
				r.Post("/discover/stream", s.migrateHandler.HandleDiscoverStream)
				r.Post("/generate", s.migrateHandler.HandleGenerate)
				r.Post("/download", s.migrateHandler.HandleDownload)
				r.Post("/export/{provider}", s.migrateHandler.HandleExportProvider)
				r.Get("/discoveries", s.migrateHandler.HandleListDiscoveries)
				r.Post("/discoveries", s.migrateHandler.HandleSaveDiscovery)
				r.Route("/discoveries/{id}", func(r chi.Router) {
					r.Get("/", s.migrateHandler.HandleGetDiscovery)
					r.Patch("/", s.migrateHandler.HandleRenameDiscovery)
					r.Delete("/", s.migrateHandler.HandleDeleteDiscovery)
				})
			})
		}

		// Deploy routes
		if s.deployHandler != nil {
			r.Route("/deploy", func(r chi.Router) {
				r.Post("/start", s.deployHandler.HandleStart)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/status", s.deployHandler.HandleStatus)
					r.Get("/stream", s.deployHandler.HandleStream)
					r.Post("/cancel", s.deployHandler.HandleCancel)
					r.Post("/retry", s.deployHandler.HandleRetry)
				})
			})
		}

		// Sync routes
		if s.syncHandler != nil {
			s.syncHandler.RegisterRoutes(r)
		}

		// Cutover routes
		if s.cutoverHandler != nil {
			s.cutoverHandler.RegisterRoutes(r)
		}

		// Providers routes
		if s.providersHandler != nil {
			s.providersHandler.RegisterRoutes(r)
		}

		// Bundle routes (for .hprt bundle export/import)
		handlers.RegisterBundleRoutes(r)

		// WebSocket routes for real-time progress
		r.Get("/ws/sync/{planId}", handlers.HandleSyncWebSocket)
		r.Get("/ws/deploy/{deploymentId}", handlers.HandleDeployWebSocket)
		r.Get("/ws/export/{bundleId}", handlers.HandleExportWebSocket)

		// Stack routes
		r.Route("/stacks/{stackID}", func(r chi.Router) {
			// Container management
			if s.dockerHandler != nil {
				r.Get("/containers", s.dockerHandler.HandleListContainers)
				r.Delete("/containers", s.dockerHandler.HandleRemoveAll)
				r.Route("/containers/{name}", func(r chi.Router) {
					r.Get("/logs", s.dockerHandler.HandleGetLogs)
					r.Post("/restart", s.dockerHandler.HandleRestart)
					r.Post("/stop", s.dockerHandler.HandleStop)
					r.Post("/start", s.dockerHandler.HandleStart)
				})
			}

			// Metrics endpoints
			if s.metricsHandler != nil {
				r.Route("/metrics", func(r chi.Router) {
					r.Get("/containers", s.metricsHandler.HandleGetContainerMetrics)
					r.Get("/containers/{containerID}", s.metricsHandler.HandleGetSingleContainerMetrics)
					r.Get("/system", s.metricsHandler.HandleGetSystemMetrics)
					r.Get("/history", s.metricsHandler.HandleGetMetricsHistory)
					r.Get("/summary", s.metricsHandler.HandleGetMetricsSummary)
				})
			}

			// Logs endpoints
			if s.logsHandler != nil {
				r.Route("/logs", func(r chi.Router) {
					r.Get("/containers/{containerID}", s.logsHandler.HandleGetContainerLogs)
					r.Get("/containers/{containerID}/stream", s.logsHandler.HandleStreamContainerLogs)
					r.Get("/search", s.logsHandler.HandleSearchLogs)
					r.Get("/stats", s.logsHandler.HandleGetLogStats)
				})
			}

			// Queues endpoints
			if s.queuesHandler != nil {
				r.Route("/queues", func(r chi.Router) {
					r.Get("/", s.queuesHandler.HandleListQueues)
					r.Route("/{queueName}", func(r chi.Router) {
						r.Get("/messages", s.queuesHandler.HandleListMessages)
						r.Delete("/", s.queuesHandler.HandlePurgeQueue)
						r.Route("/messages/{messageID}", func(r chi.Router) {
							r.Get("/", s.queuesHandler.HandleGetMessage)
							r.Post("/retry", s.queuesHandler.HandleRetryMessage)
							r.Delete("/", s.queuesHandler.HandleDeleteMessage)
						})
					})
				})
			}

			// Cache endpoints
			if s.cacheHandler != nil {
				r.Route("/cache", func(r chi.Router) {
					r.Get("/keys", s.cacheHandler.HandleListKeys)
					r.Get("/stats", s.cacheHandler.HandleGetStats)
					r.Delete("/keys", s.cacheHandler.HandleBulkDelete)
					r.Route("/keys/{key}", func(r chi.Router) {
						r.Get("/", s.cacheHandler.HandleGetKey)
						r.Put("/", s.cacheHandler.HandleSetKey)
						r.Delete("/", s.cacheHandler.HandleDeleteKey)
					})
				})
			}

			// Secrets endpoints
			if s.secretsHandler != nil {
				r.Route("/secrets", func(r chi.Router) {
					r.Get("/", s.secretsHandler.HandleListSecrets)
					r.Post("/", s.secretsHandler.HandleCreateSecret)
					r.Route("/{secretName}", func(r chi.Router) {
						r.Get("/", s.secretsHandler.HandleGetSecret)
						r.Put("/", s.secretsHandler.HandleUpdateSecret)
						r.Delete("/", s.secretsHandler.HandleDeleteSecret)
					})
				})
			}
		})
	})

	// Serve static frontend files (SPA fallback)
	staticHandler, err := StaticHandler()
	if err != nil {
		logger.Warn("Static files not available", "error", err)
	} else {
		r.NotFound(staticHandler.ServeHTTP)
	}

	s.router = r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string]string{
		"status":  "healthy",
		"version": s.config.Version,
	})
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	logger.Info("Starting server", "host", s.config.Host, "port", s.config.Port)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	logger.Info("Shutting down server gracefully...")

	// Close handlers
	if s.dockerHandler != nil {
		s.dockerHandler.Close()
	}
	if s.metricsHandler != nil {
		s.metricsHandler.Close()
	}
	if s.logsHandler != nil {
		s.logsHandler.Close()
	}
	if s.dockerService != nil {
		s.dockerService.Close()
	}
	if s.backupHandler != nil {
		s.backupHandler.Close()
	}
	if s.stacksHandler != nil {
		s.stacksHandler.Close()
	}
	if s.terminalHandler != nil {
		s.terminalHandler.Close()
	}
	if s.policyHandler != nil {
		s.policyHandler.Close()
	}

	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) Router() *chi.Mux {
	return s.router
}
