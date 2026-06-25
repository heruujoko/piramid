package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type Client struct {
	baseURL string
	client  *http.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *Client) ListTasks(ctx context.Context) ([]domain.TaskView, error) {
	var tasks []domain.TaskView
	if err := c.doJSON(ctx, http.MethodGet, "/v1/tasks", nil, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (c *Client) GetTask(ctx context.Context, taskID string) (domain.TaskView, error) {
	var task domain.TaskView
	err := c.doJSON(ctx, http.MethodGet, "/v1/tasks/"+taskID, nil, &task)
	return task, err
}

func (c *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	input any,
	output any,
) error {
	var body io.Reader
	if input != nil {
		content, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(content)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope ErrorResponse
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			return fmt.Errorf("HTTP %d", response.StatusCode)
		}
		return &APIError{
			StatusCode: response.StatusCode,
			Code:       envelope.Error.Code,
			Message:    envelope.Error.Message,
		}
	}
	if output == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(output)
}

func (c *Client) StreamEvents(
	ctx context.Context,
	lastEventID int64,
) (<-chan storepkg.Event, <-chan error) {
	events := make(chan storepkg.Event)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/events", nil)
		if err != nil {
			errs <- err
			return
		}
		if lastEventID > 0 {
			request.Header.Set("Last-Event-ID", strconv.FormatInt(lastEventID, 10))
		}
		response, err := c.client.Do(request)
		if err != nil {
			errs <- err
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			errs <- fmt.Errorf("events HTTP status %d", response.StatusCode)
			return
		}
		scanner := bufio.NewScanner(response.Body)
		var id int64
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				id, _ = strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)
			case strings.HasPrefix(line, "data: "):
				event, err := decodeEvent([]byte(strings.TrimPrefix(line, "data: ")), id)
				if err != nil {
					errs <- err
					return
				}
				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			errs <- err
		}
	}()
	return events, errs
}
