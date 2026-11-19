package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
	"github.com/Sao-Ali/vetch/internal/workload"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

const (
	backendFailure = "backend failure"
	busyBackend    = "busy backend"
	wrongResponse  = "wrong response"
	testModel      = "model-a"
)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestGatewayEligibilityAndIdentity(t *testing.T) {
	for _, name := range []string{"ready", "stale heartbeat", "stale generation", "wrong model", wrongResponse, backendFailure, busyBackend} {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			model := testModel
			now := metav1.Now()
			cluster := &infrastructurev1alpha1.VirtualInferenceCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: "default", Generation: 1},
				Spec:       infrastructurev1alpha1.VirtualInferenceClusterSpec{Model: &model, Worker: &infrastructurev1alpha1.InferenceWorkerTemplate{}},
				Status: infrastructurev1alpha1.VirtualInferenceClusterStatus{AppliedGeneration: 1, ReadyWorkers: 1, LastAgentContact: &now,
					Conditions: []metav1.Condition{{Type: "WorkersReady", Status: metav1.ConditionTrue, ObservedGeneration: 1}}},
			}
			want := http.StatusOK
			switch name {
			case "stale heartbeat":
				cluster.Status.LastAgentContact = &metav1.Time{Time: time.Now().Add(-time.Minute)}
				want = 503
			case "stale generation":
				cluster.Generation = 2
				want = 503
			case "wrong model":
				model = "model-b"
				want = 404
			case wrongResponse:
				want = 502
			case backendFailure:
				want = 502
			case busyBackend:
				want = 503
			}
			calls := 0
			gateway := &Gateway{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build(), Namespace: "default",
				HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					calls++
					if request.URL.Host != "backend-agent.default.svc:8080" {
						t.Errorf("unexpected target: %s", request.URL.Host)
					}
					if name == backendFailure {
						return nil, context.DeadlineExceeded
					}
					if name == busyBackend {
						return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"code":"capacity_exhausted","message":"busy"}`))}, nil
					}
					responseModel := testModel
					if name == wrongResponse {
						responseModel = "other"
					}
					body, _ := json.Marshal(workload.Result{ID: "request-1", Model: responseModel, WorkerID: "worker-0", Text: "hello"})
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
				})}}
			body, _ := json.Marshal(workload.Request{ID: "request-1", Model: testModel, Prompt: "hello", MaxTokens: 16, Deadline: time.Now().Add(time.Minute)})
			response := httptest.NewRecorder()
			gateway.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/infer", bytes.NewReader(body)))
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body)
			}
			if name == "ready" && !strings.Contains(response.Body.String(), `"backendID":"default/backend"`) {
				t.Fatal("missing backend identity")
			}
			if calls > 1 {
				t.Fatal("ambiguous request retried")
			}
		})
	}
}
