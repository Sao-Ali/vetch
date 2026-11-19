// Package gateway forwards complete inference requests to fresh, model-matched backends.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
	"github.com/Sao-Ali/vetch/internal/controller"
	"github.com/Sao-Ali/vetch/internal/workload"
)

type Gateway struct {
	Client     client.Reader
	Namespace  string
	HTTPClient *http.Client
}

func (gateway *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		workload.Write(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /infer", gateway.infer)
	return mux
}

func (gateway *Gateway) infer(writer http.ResponseWriter, request *http.Request) {
	var input workload.Request
	if err := workload.Decode(writer, request, &input); err != nil {
		workload.Fail(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := input.Validate(); err != nil {
		workload.Fail(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ctx, cancel := context.WithDeadline(request.Context(), input.Deadline)
	defer cancel()
	clusters := &infrastructurev1alpha1.VirtualInferenceClusterList{}
	if err := gateway.Client.List(ctx, clusters, client.InNamespace(gateway.Namespace)); err != nil {
		workload.Fail(writer, http.StatusServiceUnavailable, "discovery_failed", "backend discovery failed")
		return
	}
	matched := false
	for _, cluster := range clusters.Items {
		if cluster.Spec.Model == nil || *cluster.Spec.Model != input.Model || cluster.Spec.Worker == nil {
			continue
		}
		matched = true
		ready := meta.FindStatusCondition(cluster.Status.Conditions, "WorkersReady")
		if !cluster.DeletionTimestamp.IsZero() || cluster.Status.LastAgentContact == nil ||
			time.Since(cluster.Status.LastAgentContact.Time) > 3*controller.AgentPollInterval ||
			ready == nil || ready.Status != metav1.ConditionTrue || ready.ObservedGeneration != cluster.Generation ||
			cluster.Status.AppliedGeneration != cluster.Generation || cluster.Status.ReadyWorkers < 1 {
			continue
		}
		gateway.forward(ctx, writer, input, cluster)
		return // Never retry an ambiguous request or silently change models.
	}
	if !matched {
		workload.Fail(writer, http.StatusNotFound, "unsupported_model", "no backend is configured for the requested model")
		return
	}
	workload.Fail(writer, http.StatusServiceUnavailable, "capacity_exhausted", "no fresh, ready backend is available")
}

func (gateway *Gateway) forward(ctx context.Context, writer http.ResponseWriter, input workload.Request, cluster infrastructurev1alpha1.VirtualInferenceCluster) {
	body, _ := json.Marshal(input)
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, controller.AgentURL(cluster.Name, cluster.Namespace)+"/infer", bytes.NewReader(body))
	if err != nil {
		workload.Fail(writer, http.StatusBadGateway, "backend_error", "cannot construct backend request")
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	httpClient := gateway.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	response, err := httpClient.Do(upstream)
	if err != nil {
		code := http.StatusBadGateway
		if ctx.Err() != nil {
			code = http.StatusGatewayTimeout
		}
		workload.Fail(writer, code, "backend_error", "backend request failed or deadline expired")
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		var failure workload.Failure
		if json.NewDecoder(io.LimitReader(response.Body, workload.MaxBodyBytes)).Decode(&failure) != nil {
			workload.Fail(writer, http.StatusBadGateway, "backend_error", "backend returned an invalid error")
			return
		}
		workload.Write(writer, response.StatusCode, failure)
		return
	}
	var result workload.Result
	if json.NewDecoder(io.LimitReader(response.Body, workload.MaxBodyBytes)).Decode(&result) != nil || result.ID != input.ID || result.Model != input.Model {
		workload.Fail(writer, http.StatusBadGateway, "backend_error", "backend response identity does not match the request")
		return
	}
	result.BackendID = cluster.Namespace + "/" + cluster.Name
	workload.Write(writer, http.StatusOK, result)
}
