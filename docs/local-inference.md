# Local CPU inference

This implements the cloud completion plan's VM, worker, and request path on a
self-hosted Linux machine. Documentation alignment (step 1) is deferred.
Kubernetes remains the management API. The inference gateway accepts prompts;
prompts and model outputs are not stored in Kubernetes resources.

## Configuration and runtime

`spec.worker` enables inference. Existing VM-only and dummy-node resources keep
their previous behavior. Each worker starts a separate CPU-only llama.cpp server
with one active request. There is no request queue.

The guest includes Qwen3-0.6B Q8_0 at model revision
`23749fefcc72300e3a2ad315e1317431b06b590a`, with SHA-256
`9465e63a22add5354d9bb4b99e90117043c7124007664907259bd16d043bb031`.
llama.cpp is supplied by the existing pinned Nixpkgs revision in `flake.lock`
(package version 0.4.1). Model files are root-owned regular files; the non-root
agent validates the checksum before changing running configuration. The guest
must be rebuilt to change its packaged runtime or model artifacts.
Startup uses `--load-mode none` to load weights without mmap; this pinned
llama.cpp version no longer accepts `--no-mmap`.

The initial sample uses a 2-vCPU, 4-GiB VM and one worker limited to 1 CPU and
2 GiB of memory, with a 2,048-token context. The agent reserves one CPU and
256 MiB for the guest OS and supervisor, and rejects aggregate worker budgets
exceeding the remaining capacity. Each process starts inside its own cgroup v2
with `cpu.max`, `memory.max`, and no swap. Missing cgroup delegation fails the
worker; it never starts without limits. Runtime listeners stay on guest loopback.

## Agent contract

The agent listens on TCP 8080, reachable inside the cluster through an owned
ClusterIP Service. `GET /healthz` reports agent process health, not model readiness.

| Endpoint | Behavior |
| --- | --- |
| `GET /version` | Agent build version |
| `PUT /config` | Validate and accept desired model, generation, worker count, and per-worker settings; return HTTP 202 with observed state |
| `GET /config` | Desired/applied generations, model identity, and worker observations |
| `GET /workers`, `GET /capacity` | The same observed snapshot; READY workers can accept a request, BUSY workers are serving one |
| `POST /infer` | Execute a non-streaming, explicitly model-matched request |

Configuration changes require a newer generation. Reapplying identical settings
does not restart healthy processes. Validation failures leave the existing
configuration running. Applied generation advances after all models load.
Workers move through PROVISIONING, READY, BUSY, OFFLINE, and ERROR. Crashes and
failed readiness checks report ERROR. Failed workers are retried after a
30-second backoff when the controller next reapplies configuration; healthy
workers continue running. Agent shutdown kills worker process groups and their
cgroup descendants. Systemd also owns the complete agent process tree.

In inference mode, boot configuration is stable: the controller applies current
worker settings over HTTP instead of changing cloud-init on every update.
Changing VM CPU, RAM, or guest image still requires the KubeVirt restart workflow.

## Conditions and routing

`VMReady` reports current-generation KubeVirt readiness. VM failure, pause,
restart-required, provisioning, and deletion observations have separate reasons.
The controller also reports `AgentReachable`, `ConfigApplied`, and `WorkersReady`.
For inference-enabled resources, `Available=True` requires usable serving
capacity. A booted VM alone is insufficient. `declaredWorkerCapacity` remains
the requested count; `readyWorkers` counts healthy loaded workers, including
busy workers.

Agent observation uses a five-second total timeout and a ten-second polling
interval, in addition to owned-child watches. Status transitions preserve their
timestamps when unchanged. Serving heartbeats are rate limited to the polling
interval. The gateway requires a heartbeat no older than 30 seconds and the
current applied and observed generations. Agent admission handles races between
discovery and requests. Unsupported models return 404; busy/unready capacity
returns 503. Runtime/backend failures return 502, and execution deadline expiry
returns 504. Requests are not automatically retried after ambiguous failures.

The gateway lists resources only in its configured namespace. Its sample RBAC
permits only that list operation. Keep the agent and gateway private: the sample
uses ClusterIP Services and local Kubernetes-authenticated port forwarding.
LAN exposure, TLS/authentication, and edge discovery are subsequent work.

## Dedicated integration deployment

Requires Linux, working KVM (`/dev/kvm`), Docker, Nix, Kind, and kubectl. This
creates a disposable test cluster; it is not a persistent production deployment.
Keep the kubeconfig in ignored `bin/` and confirm the context before mutations.

```bash
mkdir -p bin
kind create cluster --name vetch-inference-test --image kindest/node:v1.35.0 \
  --config config/samples/inference-test-kind.yaml \
  --kubeconfig bin/inference-test.kubeconfig
export KUBECONFIG="$PWD/bin/inference-test.kubeconfig"
test "$(kubectl config current-context)" = kind-vetch-inference-test

kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.9.0/kubevirt-operator.yaml
kubectl wait --for=condition=Established crd/kubevirts.kubevirt.io --timeout=60s
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.9.0/kubevirt-cr.yaml
kubectl wait -n kubevirt kv/kubevirt --for=condition=Available --timeout=600s

make guest-containerdisk GUEST_IMG=vetch-guest:inference
docker build -t controller:inference .
kind load docker-image vetch-guest:inference controller:inference --name vetch-inference-test
make manifests generate kustomize
bin/kustomize build config/default |
  sed 's@image: controller:latest@image: controller:inference@' |
  kubectl apply -f -
kubectl rollout status -n vetch-system deployment/vetch-controller-manager --timeout=120s
kubectl apply -n default -f config/samples/gateway.yaml
kubectl apply -n default -f config/samples/infrastructure_v1alpha1_virtualinferencecluster_inference.yaml
kubectl wait -n default virtualinferencecluster/local-inference \
  --for=condition=Available --timeout=300s
kubectl port-forward -n default service/vetch-gateway 8082:8082
```

From another terminal, send a request with an explicit model and deadline:

```bash
jq -n --arg deadline "$(date -u -d '+90 seconds' +%Y-%m-%dT%H:%M:%SZ)" \
  '{id:"demo-1", model:"Qwen/Qwen3-0.6B", prompt:"Say hello in one sentence.", maxTokens:32, deadline:$deadline}' |
  curl --fail-with-body -sS http://127.0.0.1:8082/infer \
    -H 'Content-Type: application/json' --data-binary @-
```

Results include request/model identity, backend and worker IDs, execution device,
generated text, prompt/output token counts, and total runtime duration in ms.
Client disconnect or deadline cancellation propagates to the runtime request.
Output is limited to 128 tokens per request; thinking is disabled.

To change worker settings, edit and reapply the inference sample. To test two
1-CPU/2-GiB workers, first increase the VM to at least 3 vCPUs and 6 GiB, restart
it, then request two workers. Observe `WorkersReady`, applied generation, and the
actual guest cgroup limits. Deleting the parent relies on Kubernetes garbage
collection for its VM and Service; envtest does not run that collector.

To remove only this dedicated test environment:

```bash
test "$(kubectl config current-context)" = kind-vetch-inference-test
kind delete cluster --name vetch-inference-test
```

## Verification

```bash
make manifests generate
make lint-fix
make test
go test -race ./internal/agent ./internal/gateway ./internal/workload
docker build -t controller:inference .
make guest-qcow2
```

Controller/API tests use envtest. Worker tests exercise readiness, checksum and
budget validation, configuration generation, crash detection, admission, and
deadline cancellation. Gateway tests exercise freshness, generation/model
matching, response identity, and backend failures.

The completion plan's persistent-host setup, real host reboot, backup/restore,
protected LAN endpoint, and physical edge-board validation remain acceptance
work. A disposable Kind deployment cannot establish those guarantees.

See [validation results](local-inference-validation.md) for the checks run on
the implementation.
