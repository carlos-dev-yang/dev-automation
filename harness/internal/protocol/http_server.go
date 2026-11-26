package protocol

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// Server exposes the external HTTP+JSON & SSE ingress/egress protocol for any external adapter.
// Endpoints:
//   GET  /v1/health
//   POST /v1/messages
//   GET  /v1/events?channel=<channel>&chat_id=<optional>
type Server struct {
	transport Transport
	authToken string
	logger    *log.Logger
	mux       *http.ServeMux
}

func NewServer(transport Transport, authToken string, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	s := &Server{
		transport: transport,
		authToken: strings.TrimSpace(authToken),
		logger:    logger,
		mux:       http.NewServeMux(),
	}
	s.mux.HandleFunc("GET /v1/health", s.handleHealth)
	s.mux.HandleFunc("POST /v1/messages", s.handleMessages)
	s.mux.HandleFunc("GET /v1/events", s.handleEvents)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

// ListenAndServe starts the protocol HTTP server on addr until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("protocol server listen %s: %w", addr, err)
	}
	return s.Serve(ctx, ln)
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	s.logger.Printf("[protocol] listening on http://%s (version=%s, auth=%v)", ln.Addr().String(), Version, s.authToken != "")
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return ctx.Err()
	}
	return err
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if s.authToken == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		writeJSONError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "invalid bearer token")
		return false
	}
	return true
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	st, err := s.transport.Health(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	defer r.Body.Close()

	var req IngressMessage
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid json payload: %v", err))
		return
	}
	if strings.TrimSpace(req.Channel) == "" || strings.TrimSpace(req.ChatID) == "" {
		writeJSONError(w, http.StatusBadRequest, "channel and chat_id are required")
		return
	}

	resp, err := s.transport.Send(r.Context(), req)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	chatFilter := strings.TrimSpace(r.URL.Query().Get("chat_id"))

	ch, err := s.transport.Subscribe(r.Context(), channel)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, ": connected protocol=%s\n\n", Version)
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			if chatFilter != "" && ev.ChatID != chatFilter {
				continue
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{
		"protocol_version": Version,
		"error":            msg,
	})
}
