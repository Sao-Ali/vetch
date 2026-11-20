# Local inference validation — 2026-11-19

This records the original `be0e8eb` implementation. The subsequent configuration
authentication, ingress, and backend-admission fixes have a separate
[review follow-up and validation record](review-be0-fixes.md).

The implementation was checked on x86-64 Linux with KVM, in the dedicated
`kind-vetch-inference-test` cluster (Kubernetes 1.35.0, KubeVirt 1.9.0).
Every cluster mutation used an isolated kubeconfig under ignored `bin/` and
confirmed that context. The normal development context was not modified.

## Repository checks

These commands passed:

```bash
make manifests generate
make lint-fix
make test
make build
go test -race ./internal/agent ./internal/gateway ./internal/workload
GOOS=darwin GOARCH=arm64 go build -o bin/agent-darwin ./cmd/agent
docker build -t controller:inference .
```

`make test` uses Kubernetes 1.36.2 envtest binaries. It covers the legacy
controller, VM/inference reconciliation and ownership, current-generation
readiness, configuration validation, worker lifecycle, admission, cancellation,
and gateway eligibility/identity checks. Non-Linux builds retain health-only
agent support; resource-limited inference requires Linux.

The Nix guest and agent were built from a source export under `bin/` so new,
unstaged source files were included without staging implementation changes:

```bash
nix build path:./bin/cloud-implementation/source#vetch-agent \
  --out-link bin/cloud-implementation/final-agent
nix build path:./bin/cloud-implementation/source#guest-qcow2 \
  --out-link bin/cloud-implementation/final-guest-qcow2
```

The export contains repository sources, with local credentials, build outputs,
and ignored files excluded. Normal committed checkouts can use the Makefile
guest targets described in [the setup guide](local-inference.md).

## Live observations

| Check | Result |
| --- | --- |
| Kubernetes resource to VM | The controller created its owned VM and agent Service; KubeVirt booted the NixOS guest. |
| VM versus inference readiness | The VM reported ready while the agent/model were still unavailable; overall inference availability stayed false. |
| Real model response | Gateway returned HTTP 200 with Qwen3-0.6B text, request/model identity, backend `default/local-inference`, worker `worker-0`, token counts, and timings. |
| Resource enforcement | Guest cgroups reported `cpu.max = 100000 100000` and `memory.max = 2147483648`; one loaded worker used about 929 MB at the sampled instant. |
| Live worker update | Changing context from 2,048 to 1,024 tokens advanced the applied generation without replacing the VMI. |
| Runtime crash | Killing the runtime withdrew serving availability; the failed worker recovered after the backoff and subsequent polling (about 40 seconds observed). |
| Agent restart | Desired configuration converged again and serving availability recovered. |
| Garbage collection | Deleting the parent removed the owned VM and Service in the isolated cluster. |
| Fresh rebuilt guest | The corrected complete NixOS image booted in KubeVirt and returned a gateway response without manually replacing the agent. |
| VM restart and two workers | Increasing the VM to 3 vCPUs/6 GiB and restarting its VMI restored desired generation 2 with two ready workers. Both cgroups independently enforced 1 CPU and 2 GiB. |
| Concurrent inference | Two simultaneous requests returned HTTP 200 from different worker IDs, each generating 128 tokens (about 7.4–7.7 seconds in the final check). |
| Capacity admission | A third request while both workers were busy returned HTTP 503 with `capacity_exhausted`. |
| Unsupported model | A model with no configured backend returned HTTP 404 with `unsupported_model`. |
| Deadline | A request with a two-second deadline returned HTTP 504 and propagated cancellation to its backend request. |

The first real request produced:

```json
{"text":"Hello! How can I assist you today?","promptTokens":19,"outputTokens":10,"durationMs":759}
```

That single timing is a smoke-test observation, not a throughput benchmark.

The final guest image was streamed as an OCI containerdisk into the dedicated
Kind node to avoid retaining extra multi-gigabyte Docker build-cache copies on
the constrained workspace disk. It used `/disk/disk.qcow2` owned by UID/GID 107,
matching the repository's containerdisk packaging. The normal reproducible
Docker packaging commands are in the setup guide.

After validation, the test resource and dedicated Kind cluster were deleted.
The original Kubernetes context remained unset. Implementation changes were
left uncommitted for review.

## Remaining completion gate

Persistent central-host deployment, real host reboot recovery, configuration
backup/restore, protected LAN exposure, and VENTUNO Q compatibility/performance
still require acceptance checks before declaring the entire cloud-to-edge
handoff complete. Step 1's broader documentation alignment remains deferred.
