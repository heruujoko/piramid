package api

import (
	"net/http"
)

func (s *Server) listLoops(writer http.ResponseWriter, request *http.Request) {
	loops, err := s.application.ListLoops(request.Context())
	if err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, loops)
}

func (s *Server) listLoopFires(writer http.ResponseWriter, request *http.Request, loopID string) {
	fires, err := s.application.ListLoopFires(request.Context(), loopID)
	if err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, fires)
}
