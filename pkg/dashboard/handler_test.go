package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticRoutesServeIndex(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler(): %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(handler.Static))
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	tests := []string{"/", "/dashboard", "/dashboard/"}
	for _, path := range tests {
		req, err := http.NewRequest("GET", ts.URL+path, nil)
		if err != nil {
			t.Fatalf("NewRequest %s: %v", path, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("GET %s: unexpected status %d location %q body %q", path, resp.StatusCode, resp.Header.Get("Location"), string(body))
		}
		resp.Body.Close()
	}
}
