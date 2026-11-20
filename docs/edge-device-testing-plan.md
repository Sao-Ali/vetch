# Edge device testing plan

Status: proposed next work. The immediate milestone is running the existing
central inference demo from a MacBook over SSH. Physical edge testing follows
that demonstration. This document does not enable SSH, deploy a cluster, or
claim that edge registration, authenticated HTTPS, or edge routing exists yet.

## Current starting point

Vetch has a tested resource-to-response path on this Linux machine:

```text
VirtualInferenceCluster YAML
  -> controller -> KubeVirt VM -> guest agent -> llama.cpp worker
Client prompt -> gateway -> agent -> worker -> generated response
```

The guest packages Qwen3-0.6B Q8_0 and llama.cpp. Workers have enforced CPU/RAM
limits, readiness/state reporting, generation-based configuration, and crash
recovery. One- and two-worker configurations were exercised in an isolated Kind
cluster, which was removed after validation. There is no demo cluster currently
left running from those tests.

Use [local inference setup](local-inference.md) and
[validation results](local-inference-validation.md) as the implementation
reference. The broader documentation alignment remains deferred.

## 1. Demonstrate central inference from the MacBook

The MacBook is a remote operator/client. All Kubernetes, KubeVirt, model files,
and inference execution remain on the Linux machine. The MacBook needs SSH and
an HTTP client; it does not need to host a VM or receive the server's kubeconfig.

### Prepare the Linux machine

1. Follow [MacBook SSH setup](macbook-ssh-demo.md) for this machine's actual
   addresses, username `ali`, public host-key fingerprint, initial key enrollment,
   and the `vetch-linux` SSH alias. Keep private keys on the MacBook. Changes to
   the host's SSH configuration are separate setup work, not performed by this plan.
2. Recreate the dedicated `vetch-inference-test` cluster using the setup guide,
   with a kubeconfig under ignored `bin/`. Confirm its exact context before any
   cluster mutation. Never use a development or production cluster for failure
   or cleanup tests.
3. Build/load the guest and controller/gateway images and apply the inference
   sample. Check available scratch space: the guest QCOW2 is about 3.7 GiB, and
   packaging/loading images can retain additional copies.
4. Run the controller and gateway as Kubernetes Deployments so they remain
   running when an SSH shell disconnects. Wait for inference `Available=True`.
5. Keep the gateway private and forward its Service to Linux loopback.

After configuring the `vetch-linux` alias, connect from a MacBook terminal:

```bash
ssh vetch-linux
```

In that Linux SSH session, run:

```bash
cd /home/ali/vetch
export KUBECONFIG="$PWD/bin/inference-test.kubeconfig"
test "$(kubectl config current-context)" = kind-vetch-inference-test
kubectl get virtualinferencecluster local-inference -o yaml
kubectl port-forward --address 127.0.0.1 -n default \
  service/vetch-gateway 8082:8082
```

Keep that forwarding session open. From another MacBook terminal:

```bash
ssh -N -o ExitOnForwardFailure=yes \
  -L 127.0.0.1:18082:127.0.0.1:8082 vetch-linux
```

From a third MacBook terminal, send a prompt through the tunnel. This uses
macOS `date` syntax and requires the client/server clocks to agree:

```bash
vetch_deadline=$(date -u -v+90S '+%Y-%m-%dT%H:%M:%SZ')
curl -sS --max-time 95 -w '\nHTTP %{http_code}\n' \
  http://127.0.0.1:18082/infer \
  -H 'Content-Type: application/json' --data-binary @- <<EOF
{
  "id": "macbook-demo-1",
  "model": "Qwen/Qwen3-0.6B",
  "prompt": "Explain Kubernetes in one short sentence.",
  "maxTokens": 64,
  "deadline": "$vetch_deadline"
}
EOF
```

The SSH connection carries the request to the server's loopback listener. The
gateway then forwards it to the VM agent. Model computation happens in the VM.
Do not expose the Kubernetes API, guest agent, or llama.cpp ports publicly for
this demonstration.

### Evidence and completion criteria

- Receive generated text on the MacBook with model, backend/worker IDs, token
  counts, and timing information.
- Show the VM/VMI and current Vetch readiness conditions on the server.
- Through the QEMU guest agent, show `systemctl status vetch-agent`, its real
  llama.cpp child process, and the worker cgroup CPU/RAM limits inside the VM.
  The current guest has SSH disabled; SSH access to the host does not imply SSH
  access to the guest.
- Show the agent's `/workers` snapshot changing READY -> BUSY -> READY during
  a sufficiently long request.
- In this dedicated test environment, kill the specific runtime process and
  demonstrate failure reporting, unavailable capacity, and recovery. Do not
  treat the response's backend label alone as proof of execution location.
- Close the client/tunnel, reconnect, recreate forwarding, and issue another
  request without manually restarting the worker or controller. Losing a
  port-forward stops that connection; the Kubernetes workloads should continue.
- Record exact commands, versions, limitations, and the observed response.

Complete when the user can reproduce this SSH-based demonstration from the
MacBook. This does not establish real host reboot recovery. Permanent hosting,
reboot, backup/restore, and protected LAN access remain separate acceptance work.

## 2. Establish the edge board as another inference host

The intended deployment is:

| Component | Central Linux machine | Edge board |
| --- | --- | --- |
| Kubernetes and Vetch controller | Own desired state and observe backends | Not required for the first edge test |
| Inference gateway | Accept prompts and choose a backend | Not required |
| Vetch agent | Runs inside the KubeVirt VM | Runs directly on the board's Linux OS |
| Runtime workers | Separate CPU-limited llama.cpp processes in the VM | Separate limited native processes on the board |
| Model artifacts | Packaged in the reproducible guest image | Installed and checksum-verified on the board |

KubeVirt continues provisioning central VMs. The physical board already exists;
initial edge provisioning means installing/configuring software and starting
workers within the board's actual capacity. It does not create virtual CPUs or
RAM on the board. Applying Kubernetes YAML from the board would instead make
the board an authenticated control-plane client; that is a separate role.

### First board-local experiment — issue #25

1. Identify the board, Linux distribution, CPU architecture, usable RAM/storage,
   network connectivity, systemd version, and cgroup-v2 delegation support.
2. Build/package the agent for Linux ARM64 and the selected native runtime.
   The current x86-64 NixOS VM image is not a board installation image. Review
   the agent's executable/model paths and delegated cgroup root for the board.
3. Try the same pinned Qwen3-0.6B Q8_0 artifact on CPU first, verifying its
   checksum. If it cannot run, record the exact blocker before choosing another
   runtime or model. NPU/GPU acceleration is a separate experiment.
4. Install a non-root agent service and immutable model files. Start one worker
   with budgets based on measured board capacity, leaving OS/agent headroom.
5. Send a local request and record the runtime revision, model identity/hash,
   context/output settings, startup latency, inference latency, token counts,
   and memory/CPU observations.
6. Test runtime crash, agent restart, board reboot, and resource-limit behavior.

Complete when inference runs locally on the physical board, or a reproducible
hardware/runtime blocker is documented. Do not call an external model API and
report that as board-local inference.

## 3. Call the board from the central machine

The board agent is the backend server. It manages workers and exposes the
same configuration, observation, and non-streaming inference contract:

| Endpoint | Meaning |
| --- | --- |
| `GET /healthz` | Agent process health; not model readiness |
| `PUT /config` | Desired model, generation, worker count, and resource budgets |
| `GET /config`, `/workers`, `/capacity` | Applied generation and worker observations |
| `POST /infer` | Model ID, request ID, prompt, output limit, and deadline; return generated output and execution metadata |

Model installation/configuration happens before inference. Each prompt names a
model ID; it does not upload model weights. A loaded worker moves READY -> BUSY
-> READY, while the agent also reports PROVISIONING, OFFLINE, and ERROR. The
caller waits for the HTTP response, and health/capacity observation runs
separately. The runtime listener stays on board loopback behind the agent.

For the first central-to-board test, use an SSH tunnel with the existing HTTP
agent contract, or implement authenticated HTTPS before exposing that contract
across the network. Current agent listeners authenticate configuration writes
using a private bootstrap credential, but do not implement HTTPS or authenticate
inference/read endpoints. Certificate/credential installation and rotation require an
explicit design; never put credential values in examples or Git.

Complete when the central machine can obtain a real board-generated response
and verify unsupported-model errors, busy capacity, deadlines, cancellation,
disconnect handling, and recovery over the intended private connection.

## 4. Register and route edge capacity through Vetch

Only after the direct board experiment succeeds:

- Design a Kubernetes representation for an existing edge backend, its endpoint,
  supported models, capacity, and references to connection credentials. Use
  Kubebuilder if this introduces an API. Kubernetes remains the management API.
- Distinguish configuring an existing board from provisioning a central VM.
- Implement central configuration convergence and fresh health/worker/capacity
  observations for the board. Reject stale observations for new assignments.
- Extend gateway discovery beyond the currently supported VM backends. First
  provide a deterministic way to select central or edge execution for testing.
- Route only to backends serving the explicitly requested model. Any fallback
  must preserve model identity; never silently substitute another model.
- Stop new assignments when the board disconnects. Only retry a request when it
  is known not to have started; return a clear error after ambiguous failures.
- Keep prompts/results out of Kubernetes resources.

Complete when the same client interface reaches either the VM or board, reports
the real backend/worker/device, and demonstrates healthy-backend selection and
offline handling. This functionality is planned, not implemented in the current
VM gateway.

## 5. Compare behavior and decide the next feature

For a fair initial comparison, use the same model artifact, prompt set, context,
output limits, and concurrency on both backends. Record network-inclusive
latency separately from runtime duration; report actual CPU/RAM budgets and
token counts. Different models can be supported later under distinct model IDs.

Use measurements to choose the next feature: routing policy, board-native
acceleration, model distribution, or more robust lifecycle handling. Keep CLI
work separate from these functionality milestones.

## Issue alignment and review gates

- #20 (runtime supervision) and #24 (VM readiness): implemented and validated in
  the current local inference work; GitHub closure still needs review/merge.
- #19 (worker states): states and lifecycle tests exist; finish explicit allowed
  transition rules, invalid-transition rejection/tests, and shutdown race
  coverage before claiming its full acceptance criteria.
- #18 (validation), #14 (request/result contract), and #12 (model/runtime
  criteria): audit their remaining acceptance details independently; the
  inference demonstration alone is not proof every criterion is complete.
- #16 was already closed; #21's guest-image foundation predates this batch.
- #25 is the first physical-board investigation after the SSH demonstration.
- #28 (CLI) remains deferred.

This plan does not change GitHub issue states. Preserve the current VM contract
while adding edge support in separate, reviewable changes.
