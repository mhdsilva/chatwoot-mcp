// Package panel implements the loopback-only setup and status HTTP panel.
package panel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"chatwoot-mcp/internal/core"
	"chatwoot-mcp/web"
)

const maxSetupBody = 16 << 10

type ClientFactory func(core.Settings) core.API

type handler struct {
	store    core.Store
	factory  ClientFactory
	csrf     string
	port     int
	template *template.Template
}

// NewHandler returns the panel handler for the supplied loopback port. Host
// and Origin checks accept only 127.0.0.1 or localhost on that exact port.
func NewHandler(store core.Store, factory ClientFactory, port int) (http.Handler, error) {
	if store == nil || factory == nil {
		return nil, errors.New("panel store and client factory are required")
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("panel port must be between 1 and 65535")
	}
	index, err := template.ParseFS(web.Assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("parse panel page: %w", err)
	}
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return nil, errors.New("create panel session token")
	}
	h := &handler{store: store, factory: factory, csrf: hex.EncodeToString(tokenBytes[:]), port: port, template: index}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.page)
	mux.HandleFunc("GET /app.js", h.asset("app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /style.css", h.asset("style.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /api/status", h.status)
	mux.HandleFunc("POST /api/setup", h.setup)
	mux.HandleFunc("GET /api/client-config", h.clientConfig)
	mux.HandleFunc("GET /api/tools", h.tools)
	return h.protect(mux), nil
}

// ListenAndServe listens only on IPv4 loopback. A nonzero port is required;
// pass 0 to select an ephemeral port and use the returned listener address.
func ListenAndServe(ctx context.Context, store core.Store, factory ClientFactory, port int) error {
	if port < 0 || port > 65535 {
		return errors.New("panel port must be between 0 and 65535")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("listen on loopback: %w", err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	h, err := NewHandler(store, factory, actualPort)
	if err != nil {
		_ = listener.Close()
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	err = srv.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (h *handler) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.URL.Path == "/api/setup" || r.URL.Path == "/api/status" {
			noStore(w)
		}
		if !h.validHost(r.Host) {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			if !h.validOrigin(r.Header.Get("Origin")) {
				http.Error(w, "invalid origin", http.StatusForbidden)
				return
			}
			if !constantTokenEqual(r.Header.Get("X-CSRF-Token"), h.csrf) {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) validHost(raw string) bool {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return false
	}
	if port != strconv.Itoa(h.port) {
		return false
	}
	return strings.EqualFold(host, "127.0.0.1") || strings.EqualFold(host, "localhost")
}

func (h *handler) validOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return h.validHost(u.Host)
}

func constantTokenEqual(got, want string) bool {
	// Compare fixed-size decoded values to keep comparison timing independent of
	// the submitted token contents and length.
	a, errA := hex.DecodeString(got)
	b, errB := hex.DecodeString(want)
	if errA != nil || errB != nil || len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func (h *handler) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := h.template.Execute(w, struct{ CSRFToken string }{h.csrf}); err != nil {
		return
	}
}

func (h *handler) asset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := web.Assets.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	}
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	settings, err := h.store.Load(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Não foi possível ler a configuração local.")
		return
	}
	result := map[string]any{"connected": false, "configured": settings.Token != "", "has_token": settings.Token != "", "base_url": settings.BaseURL, "account_id": settings.AccountID}
	if settings.Token != "" {
		identity, checkErr := h.factory(settings).Check(r.Context())
		if checkErr != nil {
			result["message"] = safeError(checkErr, settings.Token)
		} else {
			result["connected"] = true
			result["account_name"] = identity.AccountName
			result["user_name"] = identity.UserName
		}
		result["last_checked"] = time.Now().UTC().Format(time.RFC3339)
	}
	jsonResponse(w, http.StatusOK, result)
}

type setupRequest struct {
	BaseURL   string `json:"base_url"`
	AccountID int64  `json:"account_id"`
	Token     string `json:"token"`
}

func (h *handler) setup(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	r.Body = http.MaxBytesReader(w, r.Body, maxSetupBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var input setupRequest
	if err := dec.Decode(&input); err != nil {
		jsonError(w, http.StatusBadRequest, "Dados de configuração inválidos.")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		jsonError(w, http.StatusBadRequest, "Dados de configuração inválidos.")
		return
	}
	settings := core.Settings{BaseURL: strings.TrimSpace(input.BaseURL), AccountID: input.AccountID, Token: input.Token}
	if strings.TrimSpace(settings.Token) == "" || settings.AccountID <= 0 || strings.TrimSpace(settings.BaseURL) == "" {
		jsonError(w, http.StatusBadRequest, "Informe URL, ID da conta e token.")
		return
	}
	if err := validateBaseURL(settings.BaseURL); err != nil {
		jsonError(w, http.StatusBadRequest, "URL inválida: use HTTPS, exceto para hosts de loopback.")
		return
	}
	identity, err := h.factory(settings).Check(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, safeError(err, settings.Token))
		return
	}
	if identity.AccountID != 0 && identity.AccountID != settings.AccountID {
		jsonError(w, http.StatusBadGateway, "A API retornou uma conta diferente da informada.")
		return
	}
	if err := h.store.Save(r.Context(), settings); err != nil {
		jsonError(w, http.StatusInternalServerError, safeError(err, settings.Token))
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "message": fmt.Sprintf("Conexão testada para %s e configuração salva.", identity.AccountName)})
}

func (h *handler) clientConfig(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	// JSON marshaling handles quotes and platform-specific executable paths.
	config := map[string]any{"mcpServers": map[string]any{"chatwoot": map[string]any{"command": "chatwoot-mcp", "args": []string{"mcp"}}}}
	jsonResponse(w, http.StatusOK, map[string]any{"config": config})
}

func (h *handler) tools(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	jsonResponse(w, http.StatusOK, map[string]any{"tools": toolsCatalog})
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func jsonError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message, "message": message})
}
func safeError(err error, token string) string {
	msg := strings.TrimSpace(strings.ReplaceAll(err.Error(), token, "[redacted]"))
	if msg == "" {
		return "Não foi possível verificar a conexão."
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("base URL must include a valid host")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if strings.EqualFold(host, "localhost") {
			return nil
		}
		ip := net.ParseIP(host)
		if ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return errors.New("base URL must use https; http is allowed only for loopback hosts")
}
