# Issue #22: VirtualInferenceCluster API fields

The API accepts an explicit VM request with `spec.vmCount`, `spec.workersPerVM`,
`spec.model`, and a `spec.vm` template containing `guestImage`, `cpuCores`, and
`memory`. The VM count is limited to zero or one until multi-VM reconciliation
exists. There are no defaults. A positive VM count requires all template and
worker fields. At zero VMs, the other settings can be omitted or retained, but
any supplied value must pass its field validation. Worker count and guest CPU
must be positive; guest memory must be a positive Kubernetes quantity. The
model and image are nonempty strings. The image is intended to be a bootable
KubeVirt containerDisk reference; the API does not verify bootability.

`spec.nodes` remains the legacy ConfigMap count. At least one of `nodes` and
`vmCount` is required. Presence of `vmCount`, including zero, selects VM mode;
`nodes` must then be zero or omitted. Legacy resources with only `nodes`
continue to reconcile dummy ConfigMaps. Converting an existing resource
requires an explicit update that removes `nodes` or sets it to zero and adds
the VM fields. During this #22 stage, VM mode creates no child resources and
reports `Available=False` with reason `VMProvisioningPending`. Any existing
owned dummy ConfigMaps from before conversion remain until issue #23 handles
their removal. The main sample remains the working legacy example; the
separate [VM example](../config/samples/infrastructure_v1alpha1_virtualinferencecluster_vm.yaml)
shows the new API shape with illustrative references.

In VM mode, `status.declaredWorkerCapacity` is `vmCount * workersPerVM`, or
zero for `vmCount: 0`. It is a calculation from the requested spec, not an
observed or serving worker count. VM readiness and inference readiness are not
reported by this stage. The `Available` condition's observed generation tracks
which spec was evaluated.

Validation was checked with envtest against the generated CRD, including a
valid one-VM update, zero VM requests, rejected missing and conflicting mode
fields, invalid counts, missing required VM settings, and invalid memory.
Commands run: `make manifests generate`, `make lint-fix`, `make test`.
