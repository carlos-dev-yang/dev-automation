package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPClient implements Transport over the external HTTP+JSON / SSE protocol.
// Any external module (including standalone Telegram bridge) can connect to a running harness over HTTP.
type HTTPClient struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
	sseClient  *http.Client
}

func NewHTTPClient(baseURL, authToken string) *HTTPClient {
	return &HTTPClient{
		baseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		authToken: strings.TrimSpace(authToken),
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
		sseClient: &http.Client{
			Timeout: 0, // Long-lived SSE stream controlled via context
		},
	}
}

func (c *HTTPClient) applyAuth(req *http.Request) {
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

func (c *HTTPClient) Health(ctx context.Context) (HealthStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/health", nil)
	if err != nil {
		return HealthStatus{}, err
	}
	c.applyAuth(req)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return HealthStatus{}, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return HealthStatus{}, err
	}
	if res.StatusCode != http.StatusOK {
		return HealthStatus{}, fmt.Errorf("protocol health failed (%d): %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var st HealthStatus
	if err := json.Unmarshal(body, &st); err != nil {
		return HealthStatus{}, err
	}
	return st, nil
}

func (c *HTTPClient) Send(ctx context.Context, msg IngressMessage) (EgressMessage, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return EgressMessage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return EgressMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return EgressMessage{}, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return EgressMessage{}, err
	}
	if res.StatusCode != http.StatusOK {
		return EgressMessage{}, fmt.Errorf("protocol send failed (%d): %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var out EgressMessage
	if err := json.Unmarshal(body, &out); err != nil {
		return EgressMessage{}, fmt.Errorf("decode protocol response: %w", err)
	}
	return out, nil
}

// Subscribe opens a reconnecting SSE stream against GET /v1/events?channel=<channel>.
func (c *HTTPClient) Subscribe(ctx context.Context, channel string) (<-chan EgressMessage, error) {
	out := make(chan EgressMessage, 64)
	go func() {
		defer close(out)
		backoff := 500 * time.Millisecond
		for {
			if ctx.Err() != nil {
				return
			}
			err := c.streamOnce(ctx, channel, out)
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				backoff = 500 * time.Millisecond
			}
			select {
			case <-time.After(backoff):
				backoff = min(backoff*2, 10*time.Second)
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (c *HTTPClient) streamOnce(ctx context.Context, channel string, out chan<- EgressMessage) error {
	endpoint := c.baseURL + "/v1/events"
	if channel != "" {
		endpoint += "?channel=" + url.QueryEscape(channel)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.applyAuth(req)

	res, err := c.sseClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("sse status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}
		var ev EgressMessage
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			continue
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return scanner.Err()
}
