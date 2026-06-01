package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/kernelcode0/logs-cleaner/internal/docker"
	"github.com/kernelcode0/logs-cleaner/internal/version"
)

type healthResponse struct {
	Status   string `json:"status"`
	Version  string `json:"version,omitempty"`
	ServerID string `json:"server_id"`
	Reason   string `json:"reason,omitempty"`
}

// HealthHandler returns an http.HandlerFunc for GET /health.
func HealthHandler(dockerClient *docker.Client, serverID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		resp := healthResponse{
			Status:   "ok",
			Version:  version.Version,
			ServerID: serverID,
		}

		if dockerClient != nil {
			if err := dockerClient.Ping(ctx); err != nil {
				resp.Status = "degraded"
				resp.Reason = "docker unreachable"
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(resp) //nolint:errcheck
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}
