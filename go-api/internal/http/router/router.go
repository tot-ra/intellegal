package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"legal-doc-intel/go-api/internal/http/handlers"
	"legal-doc-intel/go-api/internal/http/middleware"
	"legal-doc-intel/go-api/internal/logging"
)

func New(
	logger logging.Logger,
	api *handlers.API,
	readinessProbes []handlers.DependencyProbe,
	corsAllowedOrigins []string,
) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/health", handlers.Health)
	r.Get("/api/v1/readiness", handlers.Readiness(readinessProbes...))

	r.Route("/api/v1/documents", func(r chi.Router) {
		r.Post("/", api.CreateDocument)
		r.Get("/", api.ListDocuments)
		r.Route("/{document_id}", func(r chi.Router) {
			r.Get("/", api.GetDocument)
			r.Delete("/", api.DeleteDocument)
			r.Get("/content", api.GetDocumentContent)
			r.Get("/text", api.GetDocumentText)
		})
	})

	r.Route("/api/v1/contracts", func(r chi.Router) {
		r.Post("/", api.CreateContract)
		r.Get("/", api.ListContracts)
		r.Post("/search", api.SearchContracts)
		r.Route("/{contract_id}", func(r chi.Router) {
			r.Get("/", api.GetContract)
			r.Patch("/", api.UpdateContract)
			r.Delete("/", api.DeleteContract)
			r.Post("/files", api.AddContractFile)
			r.Patch("/files/order", api.ReorderContractFiles)
			r.Post("/chat", api.ChatContract)
		})
	})

	registerCheckRoutes := func(prefix string) {
		r.Route(prefix, func(r chi.Router) {
			r.Post("/clause-presence", api.CreateClauseCheck)
			r.Post("/company-name", api.CreateCompanyNameCheck)
			r.Post("/llm-review", api.CreateLLMReviewCheck)
			r.Delete("/", api.DeleteChecks)
			r.Route("/{check_id}", func(r chi.Router) {
				r.Get("/", api.GetCheck)
				r.Delete("/", api.DeleteCheck)
				r.Get("/results", api.GetCheckResults)
			})
		})
	}
	registerCheckRoutes("/api/v1/guidelines")
	registerCheckRoutes("/api/v1/checks")

	var handler http.Handler = r
	handler = middleware.CORS(handler, corsAllowedOrigins)
	handler = middleware.RequestID(handler)
	handler = middleware.AccessLog(logger, handler)

	return handler
}
