package entry

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer() *Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Config{Logger: logger})
}

func TestSwaggerRoutes(t *testing.T) {
	router := newTestServer().Handler()

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "swagger ui", method: http.MethodGet, path: "/swagger/index.html", wantStatus: http.StatusOK},
		{name: "swagger spec json", method: http.MethodGet, path: "/swagger/doc.json", wantStatus: http.StatusOK},
		{name: "raw swagger.json", method: http.MethodGet, path: "/swagger.json", wantStatus: http.StatusOK},
		{name: "raw swagger.yaml", method: http.MethodGet, path: "/swagger.yaml", wantStatus: http.StatusOK},
		{name: "health", method: http.MethodGet, path: "/health", wantStatus: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("%s %s returned %d, want %d", tc.method, tc.path, w.Code, tc.wantStatus)
			}
		})
	}
}

func TestSwaggerRedirect(t *testing.T) {
	router := newTestServer().Handler()

	req := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /swagger returned %d, want %d", w.Code, http.StatusMovedPermanently)
	}

	if got := w.Header().Get("Location"); got != "/swagger/index.html" {
		t.Fatalf("location = %q, want %q", got, "/swagger/index.html")
	}
}

func TestSwaggerDocIncludesEndpoints(t *testing.T) {
	router := newTestServer().Handler()

	req := httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	for _, endpoint := range []string{
		`"/updates"`,
		`"/updates/{id}"`,
		`"/updates/{id}/file"`,
		`"/operations"`,
		`"/internal/operations/{id}/file"`,
		`"/health"`,
	} {
		if !strings.Contains(w.Body.String(), endpoint) {
			t.Errorf("doc.json does not contain endpoint %s", endpoint)
		}
	}
}
