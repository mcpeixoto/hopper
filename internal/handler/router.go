package handler

import (
	"net/http"

	"github.com/mcpeixoto/hopper/internal/middleware"
)

// Router builds the control-plane HTTP handler: routes wrapped with CORS and the
// appropriate bearer-token guard (operator for submit/admin, node for the worker
// plane). Pass empty tokens to disable auth in development.
func Router(api *API, operatorToken, nodeToken string, corsOrigins []string) http.Handler {
	operator := middleware.RequireToken(operatorToken)
	node := middleware.RequireToken(nodeToken)

	mux := http.NewServeMux()

	// Health — no auth.
	mux.HandleFunc("GET /health", api.Health)

	// GitHub Actions webhook — authenticated by HMAC signature, not a bearer token.
	if api.GitHub != nil {
		mux.HandleFunc("POST /api/github/webhook", api.GitHubWebhook)
	}

	// Operator / submission plane.
	mux.Handle("POST /api/jobs", operator(http.HandlerFunc(api.SubmitJob)))
	mux.Handle("GET /api/jobs", operator(http.HandlerFunc(api.ListJobs)))
	mux.Handle("GET /api/jobs/{id}", operator(http.HandlerFunc(api.GetJob)))
	mux.Handle("POST /api/jobs/{id}/cancel", operator(http.HandlerFunc(api.CancelJob)))
	mux.Handle("POST /api/jobs/{id}/release", operator(http.HandlerFunc(api.ReleaseJob)))
	mux.Handle("GET /api/workers", operator(http.HandlerFunc(api.ListWorkers)))
	mux.Handle("PUT /api/jobs/{id}/input", operator(http.HandlerFunc(api.PutInput)))
	mux.Handle("GET /api/jobs/{id}/result", operator(http.HandlerFunc(api.GetResult)))
	mux.Handle("GET /api/jobs/{id}/logs", operator(http.HandlerFunc(api.GetLogs)))

	// Worker plane.
	mux.Handle("POST /api/jobs/claim", node(http.HandlerFunc(api.ClaimJob)))
	mux.Handle("POST /api/jobs/{id}/complete", node(http.HandlerFunc(api.CompleteJob)))
	mux.Handle("GET /api/jobs/{id}/input", node(http.HandlerFunc(api.GetInput)))
	mux.Handle("PUT /api/jobs/{id}/output", node(http.HandlerFunc(api.PutOutput)))
	mux.Handle("PUT /api/jobs/{id}/logs", node(http.HandlerFunc(api.PutLogs)))
	mux.Handle("POST /api/workers/register", node(http.HandlerFunc(api.RegisterWorker)))
	mux.Handle("POST /api/workers/{id}/heartbeat", node(http.HandlerFunc(api.Heartbeat)))

	return middleware.CORS(corsOrigins)(mux)
}
