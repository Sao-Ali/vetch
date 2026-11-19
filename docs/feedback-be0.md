# Review feedback for `feat/finish-cloud-stuff`

Reviewed commit: `be0e8eb` against `main` at `e33ba59`.

The branch demonstrates the intended local path from a
`VirtualInferenceCluster` to a VM, managed llama.cpp workers, and a gateway
response. The dedicated-cluster validation in `local-inference-validation.md`
records a real model response, resource limits, failure recovery, and two-worker
admission. The reviewer did not rerun that live deployment.

## Findings

### 1. Protect the agent configuration endpoint before shared-cluster use (high)

`internal/agent/manager.go` exposes `PUT /config` without authenticating the
caller. `internal/controller/virtualinferencecluster_vm.go` exposes the agent on
the VM's TCP port 8080, and the controller creates a ClusterIP Service for it.
ClusterIP limits external exposure but does not restrict other workloads in the
cluster from reaching the endpoint.

A caller with network access can submit a configuration with a higher generation.
The agent then rejects the controller's current configuration because `Apply`
requires a newer generation for subsequent changes. This can prevent desired
state from converging until the Kubernetes resource generation catches up or the
agent restarts. The same endpoint can change the active model and worker budget.

Restrict access to the controller and authenticate configuration writes. Add a
test that an unauthorized caller cannot advance the agent's generation.

### 2. Select available capacity across matching backends (medium)

`internal/gateway/gateway.go` forwards to the first eligible cluster for a model
and returns that backend's response. Eligibility uses `readyWorkers`, which
includes BUSY workers. If the first backend is saturated, it can return 503 even
when another matching backend has a free worker. This matters as soon as a model
has both central and edge backends, or multiple local backends.

Add a test with two matching backends, one busy and one free. Report free
capacity separately from loaded workers, or try another backend only after an
explicit `capacity_exhausted` response. Preserve the existing rule against
retrying a request after an ambiguous execution failure.

### 3. Align project documentation and keep follow-up changes narrow (medium)

The implementation commit adds the API, controller polling, process supervisor,
gateway, guest packaging, and tests in one change. The components are separated
in code, but this scope makes regressions and design decisions harder to review.
`README.md`, `docs/architecture.md`, and `AGENTS.md` still describe an earlier
stage of the product and do not consistently identify the new local inference
path as implemented behavior.

Update the project-level documentation and scope guidance to match the accepted
direction. Split subsequent fixes into focused, independently reviewable changes.

## Product assessment and remaining gate

The implementation follows the self-hosted cloud milestone in
`cloud-completion-plan.md`. Ownership checks, current-generation readiness,
explicit resource limits, and dedicated-cluster testing are solid foundations.
Persistent-host recovery, protected network access, backup and restore, and
physical edge-device compatibility remain acceptance work before the broader
cloud-to-edge handoff is complete.

## Review verification

Run in a clean checkout of `feat/finish-cloud-stuff`:

```text
make test
make lint
go test -race ./internal/agent ./internal/gateway ./internal/workload
git diff --check main...HEAD
```

All passed; `make lint` reported zero issues. The live Kind checks above are
reported by the branch's validation record and were not repeated in this review.
