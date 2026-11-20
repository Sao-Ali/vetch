# Vetch agent guide

## Scope and workflow

Vetch is a self-hosted Kubernetes platform for CPU inference infrastructure.
VirtualInferenceCluster supports legacy spec.nodes ConfigMap placeholders and
spec.vmCount (zero or one) KubeVirt VMs. Optional spec.worker enables managed
llama.cpp processes inside the NixOS guest, resource limits, agent configuration
convergence, serving readiness, and HTTP inference through the gateway.

Kubernetes is the management API. Prompts/results travel through the gateway,
agent, and runtime, never through Kubernetes resources. This machine is the
central server; physical edge registration/routing, the CLI, and persistent-host
acceptance are follow-up work. AWS is not required for the current workflow.
Do not add other infrastructure providers, runtimes, accelerator abstractions,
databases, a management REST backend, a frontend, or monitoring until requested.

Keep changes scoped, incremental, and verified. Document behavior changes and
the commands used to validate them.
Avoid speculative abstractions or dependencies. Respect planning-only requests.
Do not stage, commit, push, or deploy unless authorized.

## Layout and generation

- api/v1alpha1: API definitions and registration.
- cmd/main.go: controller-manager entry point.
- cmd/agent, internal/agent: guest agent, configuration authentication, and worker supervision.
- cmd/gateway, internal/gateway: model-matched inference admission and forwarding.
- internal/workload: shared HTTP request/result and agent configuration types.
- internal/controller: reconciliation and tests.
- guest: NixOS guest and containerdisk packaging; flake.nix pins build inputs.
- config/samples: editable examples.
- skills: repository-specific agent workflows; keep each skill self-contained and
  validate changed skills with the skill-creator validator when available.
- config/crd/bases, config/rbac/role.yaml, config/webhook/manifests.yaml,
  and **/zz_generated.*.go are generated; never hand-edit them.
- PROJECT is managed by Kubebuilder; use the CLI to update it.
- Keep scaffold markers and the existing directory layout.
- Scaffold new APIs and webhooks with kubebuilder create.

## Implementation

- Reconciliation must be idempotent; read desired state on every call.
- Treat a missing parent as normal; return other errors for retry.
- Use structured logging with capitalized messages, no trailing periods,
  balanced key/value pairs, and no secrets.
- Use Kubernetes types such as metav1.Condition and metav1.Time.
- ConfigMap children belong in the parent's namespace, with deterministic
  names, labels, and controller OwnerReferences.
- Check ownership before changes; never adopt or delete unrelated objects
  merely because names or labels match.
- Watch owned children with Owns; do not use sleep-driven workflows.
- Re-fetch before updates and avoid unnecessary writes.
- Use finalizers only when external cleanup requires them; ConfigMap children
  can use Kubernetes garbage collection.
- Scope RBAC to the operations actually implemented.
- Keep owned VMs, Services, Secrets, and NetworkPolicies in the parent's
  namespace and check controller ownership before modifying or removing them.
- Configuration writes must fail closed without the bootstrap credential.
  Keep credentials in owned Secrets and private guest files, never inline VM
  userdata, ConfigMaps, status, logs, examples, or source control.
- NetworkPolicy restrictions require a supporting CNI; do not claim policy
  enforcement from envtest or from merely creating a policy object.
- Retry another backend only after an explicit admission rejection. Do not
  retry requests whose execution outcome is ambiguous or substitute model IDs.

## Verification

After API fields or markers change:

```bash
make manifests generate
```

After Go changes:

```bash
make lint-fix
make test
```

Use envtest for API/controller tests. It does not run the garbage collector;
verify garbage collection in an isolated Kind cluster when implemented.
Future end-to-end tests must use a dedicated test cluster, never a dev/prod cluster.

Local development uses kind-vetch-dev and make run. Confirm the Kubernetes
context before mutations. Repository-only cleanup must not alter the cluster.

## Repository hygiene

Keep generated CRDs and DeepCopy code with source for reviewable checkouts.
Keep local tools in ignored bin/ and coverage outputs out of Git.
Never commit kubeconfigs, private keys, tokens, environment secrets, or local
credential directories. Inspect candidate files without exposing secret values.
Preserve unrelated user changes.
