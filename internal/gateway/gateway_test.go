package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
	"github.com/Sao-Ali/vetch/internal/workload"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

const (
	backendFailure       = "backend failure"
	busyBackend          = "busy backend"
	wrongResponse        = "wrong response"
	testModel            = "model-a"
	gatewayTestNamespace = "default"
	testPrompt           = "hello"
)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type orderedReader struct{ client.Reader }

func (reader orderedReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if err := reader.Reader.List(ctx, list, opts...); err != nil {
		return err
	}
	clusters := list.(*infrastructurev1alpha1.VirtualInferenceClusterList)
	slices.SortFunc(clusters.Items, func(a, b infrastructurev1alpha1.VirtualInferenceCluster) int { return strings.Compare(a.Name, b.Name) })
	return nil
}

func TestGatewayRetriesOnlyExplicitAdmissionRejection(t *testing.T) {
	for _, test := range []struct {
		name                  string
		status                int
		body                  string
		transportError        bool
		wantStatus, wantCalls int
	}{
		{"busy then free", 503, `{"code":"capacity_exhausted","message":"busy"}`, false, 200, 2},
		{"all busy", 503, `{"code":"capacity_exhausted","message":"busy"}`, false, 503, 2},
		{"ambiguous unavailable", 503, `{"code":"runtime_error","message":"failed"}`, false, 503, 1},
		{"ambiguous gateway failure", 502, `{"code":"capacity_exhausted","message":"failed"}`, false, 502, 1},
		{"malformed failure", 503, `broken`, false, 502, 1},
		{"transport failure", 0, "", true, 502, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			model := testModel
			now := metav1.Now()
			first := &infrastructurev1alpha1.VirtualInferenceCluster{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: gatewayTestNamespace, Generation: 1},
				Spec: infrastructurev1alpha1.VirtualInferenceClusterSpec{Model: &model, Worker: &infrastructurev1alpha1.InferenceWorkerTemplate{}},
				Status: infrastructurev1alpha1.VirtualInferenceClusterStatus{AppliedGeneration: 1, ReadyWorkers: 1, LastAgentContact: &now,
					Conditions: []metav1.Condition{{Type: "WorkersReady", Status: metav1.ConditionTrue, ObservedGeneration: 1}}}}
			second := first.DeepCopy()
			second.Name = "b"
			calls := 0
			input := workload.Request{ID: "capacity-test", Model: model, Prompt: testPrompt, MaxTokens: 16, Deadline: time.Now().Add(time.Minute)}
			gateway := &Gateway{Client: orderedReader{fake.NewClientBuilder().WithScheme(scheme).WithObjects(first, second).Build()}, Namespace: gatewayTestNamespace,
				HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					calls++
					var forwarded workload.Request
					if err := json.NewDecoder(request.Body).Decode(&forwarded); err != nil {
						t.Fatal(err)
					}
					if forwarded.ID != input.ID || forwarded.Model != input.Model || !forwarded.Deadline.Equal(input.Deadline) {
						t.Fatal("fallback changed request identity or deadline")
					}
					if calls == 1 || test.name == "all busy" {
						if calls == 1 && request.URL.Host != "a-agent.default.svc:8080" {
							t.Fatal("first backend was not exercised")
						}
						if test.transportError {
							return nil, errors.New("connection lost")
						}
						return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
					}
					body, _ := json.Marshal(workload.Result{ID: input.ID, Model: model, Text: testPrompt, WorkerID: "worker-0"})
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
				})}}
			body, _ := json.Marshal(input)
			response := httptest.NewRecorder()
			gateway.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/infer", bytes.NewReader(body)))
			if response.Code != test.wantStatus || calls != test.wantCalls {
				t.Fatalf("status=%d calls=%d; want status=%d calls=%d", response.Code, calls, test.wantStatus, test.wantCalls)
			}
			if test.wantStatus == 200 && !strings.Contains(response.Body.String(), `"backendID":"default/b"`) {
				t.Fatal("response did not identify fallback backend")
			}
		})
	}
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
				ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: gatewayTestNamespace, Generation: 1},
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
			gateway := &Gateway{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build(), Namespace: gatewayTestNamespace,
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
					body, _ := json.Marshal(workload.Result{ID: "request-1", Model: responseModel, WorkerID: "worker-0", Text: testPrompt})
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
				})}}
			body, _ := json.Marshal(workload.Request{ID: "request-1", Model: testModel, Prompt: testPrompt, MaxTokens: 16, Deadline: time.Now().Add(time.Minute)})
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
