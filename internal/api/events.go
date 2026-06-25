package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	storepkg "github.com/heruujoko/piramid/internal/store"
)

func (s *Server) events(writer http.ResponseWriter, request *http.Request) {
	after := int64(0)
	if value := request.Header.Get("Last-Event-ID"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			writeError(writer, http.StatusBadRequest, "invalid_request", "invalid Last-Event-ID")
			return
		}
		after = parsed
	}
	events, err := s.application.ListEvents(request.Context(), after, 1000)
	if err != nil {
		handleError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)
	for _, event := range events {
		content, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(writer, "id: %d\nevent: %s\ndata: %s\n\n",
			event.ID, event.EntityType, content)
	}
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

func decodeEvent(content []byte, id int64) (storepkg.Event, error) {
	var event storepkg.Event
	if err := json.Unmarshal(content, &event); err != nil {
		return storepkg.Event{}, err
	}
	event.ID = id
	return event, nil
}
