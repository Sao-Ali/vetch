# Finish the self-hosted cloud foundation before edge work

Status: planning draft recovered from the project discussion on 2026-11-19.
This document records proposed work, not completed functionality. Issue status,
model artifact details, and hardware compatibility must be checked during implementation.

The primary milestone is **apply a resource → VM boots → agent starts a worker → get a real local response**. Then make that path reliable enough that edge integration becomes adding another backend.

This plan keeps this machine as the central server, defers the CLI, and removes AWS from the required work.

**Model choice: Qwen3-0.6B with llama.cpp**

Use the official `Qwen3-0.6B-Q8_0.gguf` artifact. It is about 639 MB, published under Apache 2.0, and documented for llama.cpp. That makes it a manageable first infrastructure workload. [Official model and files](https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/tree/main).

Start with these **proposed settings**, then adjust from measurements:

| Setting | Initial target |
|---|---|
| Execution | CPU only |
| VM | 2 vCPUs, 4 GiB RAM |
| Workers | One runtime process, one active request |
| Worker budget | 1 CPU, 2 GiB RAM |
| Context | 2,048 tokens |
| Maximum generated output | 128 tokens |
| Thinking | Disabled |
| Reproducibility | Pin runtime revision, model revision, and model checksum |

The memory budget includes runtime overhead; the model’s file size is not its total RAM requirement.

VENTUNO Q runs Linux on an Arm processor, so testing this model/runtime there later is a reasonable starting point. **Compatibility and performance still need validation on the actual board; NPU acceleration is separate work.** [Arduino documentation](https://docs.arduino.cc/hardware/ventuno-q).

Devices can use different models. **Each request names its model, and only backends serving that model are eligible.** Comparing hardware performance fairly will still require a shared model and workload.

**1. Align the project’s direction and acceptance criteria**

First, update the docs around the chosen direction:

- This machine hosts the central control plane and local inference backend.
- Kubernetes remains the management API.
- KubeVirt hosts the local inference VM.
- Edge devices later expose a compatible inference interface.
- CLI #28 and edge #25 remain deferred.
- AWS becomes optional future deployment work.
- Separate implemented behavior from planned behavior, including in `AGENTS.md`.
- Reconcile #21’s open issue status with its merged implementation and recorded validation.

**Complete when:** the README, architecture, roadmap, and development guide describe the same system and identify the remaining work accurately.

This should be a small documentation change, separate from implementation.

**2. Finish VM status — issue #24**

Complete the existing VM lifecycle before extending the agent:

- Report provisioning, ready, degraded, and failure observations with useful reasons.
- Avoid rewriting unchanged status.
- Report missing KubeVirt, image/boot problems, and ownership conflicts clearly.
- Keep VM readiness distinct from agent health and inference readiness.
- Document which configuration changes require restarting the VM.

**Complete when:** a VM that cannot boot never looks available, and a booted VM does not imply a model is serving.

Validate controller behavior with envtest and actual boot behavior in a dedicated test cluster.

**3. Freeze the first workload and its interfaces — issues #12 and #14**

Record the chosen model/runtime and define the minimum inference contract.

A request should include:

- Request ID, model ID, input/messages, generation settings, output limit, and deadline.

A result should include:

- Generated output, actual model identity, backend ID, worker ID, execution device, token counts, timings, and any error.

Define cancellation, unsupported-model errors, and capacity errors. Keep the first response non-streaming to reduce implementation scope.

Also resolve the configuration gap: the API currently declares **VM resources and worker count**, but does not express complete per-worker resource budgets or runtime/model artifact configuration.

**Complete when:** the operator, agent, and future edge adapter have a documented contract with examples, and every required setting has a clear owner.

**4. Build one properly managed worker — issues #18, #19, and #20**

Implement this incrementally:

| Work | Required behavior |
|---|---|
| Configuration validation — #18 | Reject invalid IDs, paths, model artifacts, CPU/RAM budgets, and configurations exceeding available capacity. |
| Worker states — #19 | Implement and test `PROVISIONING`, `READY`, `BUSY`, `OFFLINE`, and `ERROR`. |
| Process supervision — #20 | Start and stop llama.cpp, detect crashes, and clean up its child processes. |
| Resource enforcement — follow-up | Apply actual Linux CPU/RAM limits to the runtime process. Thread settings alone are insufficient. |
| Guest packaging — follow-up | Include the pinned runtime and make the verified model artifact available predictably. |

For the first demonstration, packaging the small model into the reproducible guest is a reasonable simplification. Model updates would require an image rebuild; a persistent model cache can follow when needed.

Use systemd/cgroup facilities for enforcement, with a narrow delegation design that preserves the non-root agent.

**Complete when:** the agent starts a real runtime inside its budget, marks it ready only after loading succeeds, reports crashes, and leaves no orphaned process after shutdown.

A direct test request should produce real generated text at this stage.

**5. Connect desired Kubernetes state to the running agent**

Implement the contract designed in #16:

- Bootstrap JSON initializes the agent.
- `PUT /config` applies subsequent changes.
- `GET /config` reports desired and applied generations.
- Worker and capacity endpoints report observed state.
- The operator discovers a reachable agent endpoint and polls with bounded timeouts and retry delays.
- Unreachable agents cannot retain apparently current serving capacity.
- Agent restarts trigger configuration convergence.

Separate observations such as `VMReady`, `AgentReachable`, `ConfigApplied`, and `WorkersReady`. Overall availability should reflect whether the requested serving capacity is actually usable.

**Complete when:** changing a supported worker setting in the Kubernetes resource reaches the running agent without manually editing the guest. VM hardware/image changes may still require a documented restart.

**6. Deliver the complete resource-to-response path**

Add a minimal inference gateway on the central machine:

- Discover eligible local workers from observed state.
- Forward requests to the selected worker.
- Return generated output and execution metadata.
- Enforce deadlines and cancellation.
- Return a clear capacity error when the worker is busy; start without a queue.
- Keep runtime ports private and expose one documented client endpoint.

The first complete demonstration becomes:

```text
Apply VirtualInferenceCluster YAML
                  ↓
       Operator provisions VM
                  ↓
       Agent loads limited worker
                  ↓
       Serving readiness reported
                  ↓
  curl gateway → generated response
```

**Complete when:** a fresh deployment follows this path using documented commands, without manually starting the runtime inside the guest.

This is the main milestone. The CLI adds convenience later.

**7. Prove the foundation is ready for edge integration**

Before shifting the main implementation effort to the board, verify:

- A small repeatable request batch returns real responses and recorded timings.
- Concurrent requests receive correct admission behavior.
- A second independently limited worker can be exercised within this host’s capacity.
- Taking a worker offline prevents new assignments.
- Killing a runtime produces accurate failure status and documented recovery.
- Restarting the agent and VM restores the intended configuration.
- Rebooting the host restores the central service without an open terminal.
- Images and model artifacts remain available after restart.
- Configuration backup and recovery are documented.
- The intended LAN/private-network access path works.
- Tests continue to use an isolated cluster, separate from the persistent service.

The persistent deployment must use a documented, tested Kubernetes/KubeVirt setup. The existing `make run` development workflow is not the final service arrangement.

**Complete when:** we have one repeatable acceptance report covering these checks, with pinned versions, exact commands, measured resource use, and known limitations. No particular tokens-per-second target is required to begin edge work.

**What starts after that gate**

The first edge task is deliberately small:

> Run a model locally on VENTUNO Q, expose the agreed request/result interface, and successfully call it from this machine.

Then add model/capability discovery, health checks, and routing between local and edge backends. The board does not need Kubernetes or KubeVirt.

**The immediate next implementation task is #24**, following the documentation alignment. After that, proceed through model/interface decisions → managed worker → operator integration → gateway → acceptance checks. Keep those as separate, reviewable changes.
