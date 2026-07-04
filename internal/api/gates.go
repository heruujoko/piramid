package api

import (
	"net/http"

	"github.com/heruujoko/piramid/internal/domain"
)

func (s *Server) listGates(writer http.ResponseWriter, request *http.Request) {
	gates, err := s.application.ListOpenGates(request.Context())
	if err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, gates)
}

func (s *Server) getGate(writer http.ResponseWriter, request *http.Request, gateID string) {
	gate, err := s.application.GetGate(request.Context(), gateID)
	if err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, gate)
}

func (s *Server) resolveGate(writer http.ResponseWriter, request *http.Request, gateID string) {
	var input domain.GateDecisionInput
	if err := decodeJSON(writer, request, &input); err != nil {
		return
	}
	if err := s.application.ResolveGate(request.Context(), gateID, input); err != nil {
		handleError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
