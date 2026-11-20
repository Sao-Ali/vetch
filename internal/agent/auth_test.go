package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sao-Ali/vetch/internal/workload"
)

func TestConfigurationWritesRequireCredentialBeforeDecoding(t *testing.T) {
	manager := NewManager(context.Background(), t.TempDir(), nil, 0, 0)
	defer manager.Close()
	initial := workload.Config{Generation: 1, Model: "original", WorkersPerVM: 1}
	if err := manager.Apply(initial); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	body, _ := json.Marshal(workload.Config{Generation: 999, Model: "untrusted", WorkersPerVM: 1})
	for _, test := range []struct {
		credential, header string
		body               []byte
	}{
		{token, "", body}, {token, "Bearer wrong", body}, {token, "", []byte("not json")},
		{"", "Bearer ", body},
	} {
		request := httptest.NewRequest(http.MethodPut, "/config", bytes.NewReader(test.body))
		request.Header.Set("Authorization", test.header)
		response := httptest.NewRecorder()
		manager.Handler(test.credential).ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized write status = %d", response.Code)
		}
		if manager.Status().DesiredGeneration != initial.Generation {
			t.Fatal("unauthorized caller advanced generation")
		}
	}
	initial.Generation = 2
	contents, _ := json.Marshal(initial)
	request := httptest.NewRequest(http.MethodPut, "/config", bytes.NewReader(contents))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	manager.Handler(token).ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || manager.Status().AppliedGeneration != 2 {
		t.Fatal("controller could not converge after rejected unauthorized writes")
	}
}

func TestLoadConfigToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config-token")
	if token, err := LoadConfigToken(path); err != nil || token != "" {
		t.Fatal("missing credential should disable HTTP configuration")
	}
	for _, valid := range []bool{false, true} {
		contents := "malformed"
		if valid {
			contents = strings.Repeat("b", 64) + "\n"
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		token, err := LoadConfigToken(path)
		if valid && (err != nil || len(token) != 64) {
			t.Fatal("valid credential rejected")
		}
		if !valid && err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
}
