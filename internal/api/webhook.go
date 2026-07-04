package api

import (
	"io"
	"net/http"
)

func (s *Server) handleGitHubWebhook(writer http.ResponseWriter, request *http.Request) {
	payload, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxRequestBytes))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "failed to read body")
		return
	}
	eventType := request.Header.Get("X-GitHub-Event")
	signature := request.Header.Get("X-Hub-Signature-256")

	if err := s.application.HandleGitHubWebhook(request.Context(), eventType, signature, payload); err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}
