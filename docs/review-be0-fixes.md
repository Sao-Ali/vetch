# Follow-up to the be0e8eb review

Feedback source: `origin/feedback/be0`, `docs/feedback-be0.md`, commit
`0402490`. All three findings are valid. The fixes are grouped by configuration
access, backend admission, and documentation; the original published commit has
not been rewritten.

## Configuration access

`PUT /config` now requires a private per-cluster bearer credential. The
controller generates 32 random bytes and stores the encoded credential and
bootstrap userdata in an owned, immutable Secret. KubeVirt receives a Secret
reference, not inline credential-bearing userdata. Cloud-init creates
`/etc/vetch/config-token` with `root:vetch` ownership and `0440` permissions.

The agent rejects missing/incorrect credentials before decoding a change. A
missing credential file leaves HTTP configuration disabled. Controller requests
send the credential without adding it to configuration JSON, status, or logs.
Replacing a credential Secret changes the VM template's bootstrap UID; rotation
requires restarting the VM. See [upgrade instructions](local-inference.md).

An owned ingress NetworkPolicy selects the VM's pods and permits TCP 8080 from
the configured controller namespace/pod labels and same-namespace gateway pods.
The controller namespace comes from the deployment's downward-API
`POD_NAMESPACE`, with `vetch-system` as the host-run default. Owned-resource
checks protect unrelated Secrets and policies; normal deletion uses Kubernetes
garbage collection, with explicit cleanup for disabled inference.

NetworkPolicy requires a supporting network plugin; creating the API object
alone does not establish isolation. The live checks below exercised this setup,
not every CNI. [Kubernetes NetworkPolicy prerequisites](https://kubernetes.io/docs/concepts/services-networking/network-policies/#prerequisites).
TLS and inference/read authentication remain separate work before untrusted
network exposure.

## Backend admission

The gateway tries another fresh, matching backend only after HTTP 503 with the
explicit `capacity_exhausted` code. This is an admission rejection before model
execution. The original request ID, model, and deadline are preserved.

Transport errors, other 503/502 errors, malformed responses, and response identity
mismatches remain terminal. They can follow execution and must not trigger a
duplicate request. An all-busy set still returns a clear capacity error.

## Documentation alignment

README, architecture, scope guidance, roadmap, documentation navigation, and the
setup/edge guides now distinguish implemented local inference from planned edge
routing and persistent-host acceptance. AWS is optional future deployment work.
The next user-facing demonstration remains the MacBook SSH workflow.

## Verification

| Result | Check |
| --- | --- |
| PASS | `make manifests generate`, including generated Secret/NetworkPolicy RBAC |
| PASS | `make lint-fix` (zero issues), `make test`, and `make build` |
| PASS | `go test -race ./internal/agent ./internal/gateway ./internal/workload` |
| PASS | `go mod tidy -diff` and manifest rendering with `bin/kustomize build` |
| PASS | Unauthorized high-generation writes leave agent state unchanged; a valid subsequent configuration still converges |
| PASS | Envtest checks private Secret references, credential/policy ownership, idempotence, rotation generation, namespace selection, and cleanup |
| PASS | Gateway regression checks busy-then-free and all-busy backends, preserved request identity/deadline, and no retries after ambiguous failures |
| PASS | Fresh NixOS guest build and boot in isolated `kind-vetch-review-be0-test`, Kubernetes 1.35.0 / KubeVirt 1.9.0 |
| PASS | An untrusted pod's direct Service-IP connection was blocked; a permitted network peer without the credential received HTTP 401 |
| PASS | The live attempted generation 9999 write left generation 1 unchanged; guest credential permissions were `root:vetch 440` |
| PASS | A subsequent gateway request returned HTTP 200 and real Qwen3 output with backend/worker identity and timing |

The successful post-attack smoke response generated 10 output tokens in 511 ms.
This is a functional observation, not a benchmark. No credential values were
printed or added to source control. Test artifacts and kubeconfigs are under
ignored `bin/`. Host SSH configuration and persistent deployment were not changed.
The test parent and its owned VM, Service, Secret, and NetworkPolicy were removed
by garbage collection, then the dedicated Kind cluster and temporary controller
image were deleted. The normal Kubernetes context remains unset.
