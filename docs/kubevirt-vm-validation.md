# KubeVirt VM behavior and boot validation

This guide explains the single-VM implementation on `issue-23-reconcile-vm`
and records the Linux/KVM validation performed on 2026-10-13 against commit
`7fce1e9`. The repository checks and real VM lifecycle checks passed.

## What booting means

Vetch creates a KubeVirt `VirtualMachine` (VM), which describes the desired
virtual computer. KubeVirt creates a `VirtualMachineInstance` (VMI), its running
instance, and a launcher pod. On the tested Linux host, QEMU used hardware KVM
to start the guest operating system. Booting means that the guest kernel and
operating-system services started; it is more than creating Kubernetes objects.

The test observed Fedora reaching its serial-console login prompt and used the
QEMU guest agent to execute commands inside the guest. Libvirt's domain XML
reported `type='kvm'`, and the guest boot console identified KVM.

Vetch reports `Available=True` with reason `VMReady` when KubeVirt reports the
current VM generation ready. This can happen before guest cloud-init finishes.
It does not establish that inference workers are running or a model is serving.

## Where Fedora comes from and how resources are selected

The [VM sample](../config/samples/infrastructure_v1alpha1_virtualinferencecluster_vm.yaml)
requests a bootable disk packaged as an OCI container image:

```yaml
spec:
  vmCount: 1
  workersPerVM: 2
  model: example/model-v1
  vm:
    guestImage: quay.io/containerdisks/fedora:latest
    cpuCores: 2
    memory: 4Gi
```

Fedora is the operating system on that guest disk. KubeVirt provides the VM
infrastructure; Fedora is supplied by the selected image, rather than being
built into KubeVirt. Pulling and booting this image does not install Fedora
onto the host or replace its operating system.

Vetch maps `cpuCores` and `memory` into the KubeVirt guest CPU and memory
settings. The test guest reported two CPUs and `MemTotal: 3997016 kB` for the
4 GiB request; usable memory is slightly below the configured total. These are
virtual CPU resources backed by the host, not dedicated physical cores. Guest
memory is separate from the extra resources needed to run the VM infrastructure.

The sample's `latest` image resolved to Fedora Linux 44 during this test:

```text
quay.io/containerdisks/fedora@sha256:1217dcc8c58dbc3ca08e0323929b86f609506121adb01d0fd892232f2c0544f6
```

That records the image tested; a future pull of `latest` can resolve differently.

## Accessing and using the guest

SSH is possible with KubeVirt, but this branch does not provision guest SSH
credentials, authorized keys, or an external SSH endpoint. A usable SSH path
also requires an SSH server and login account in the guest. The sample does
not provide ready-to-use SSH access, and SSH login was not tested.

The validation instead used:

- `virtctl console` to observe guest boot through the serial console.
- The QEMU guest agent, accessed through `virsh qemu-agent-command` in the
  launcher pod, to read guest files and inspect CPU and memory.

These guest-agent checks require an installed, connected agent and appropriate
cluster permissions. They are not an SSH session. See KubeVirt's
[VM access guide](https://kubevirt.io/user-guide/user_workloads/accessing_virtual_machines/)
for SSH and console options.

A booted guest can run ordinary Linux programs. With additional setup, it
could install an inference runtime, download model files, start workers, and
serve requests. This branch provisions the guest and writes configuration;
it does not perform those inference steps. `workersPerVM` is a declared count,
and `model` is an identifier, not proof of running processes or downloaded files.

The root disk currently uses `containerDisk`, not persistent storage. Guest
changes can survive an in-guest reboot but do not survive replacement of the
VMI. A KubeVirt restart recreates the running instance, so installed packages,
downloaded models, and other guest-side changes are not durable in this setup.
See the [containerDisk documentation](https://kubevirt.io/user-guide/storage/disks_and_volumes/#containerdisk).

## Why configuration changes need an explicit restart

Vetch renders `model` and `workersPerVM` into cloud-init user data. On guest
initialization, cloud-init writes `/etc/vetch/config.json`:

```json
{"model":"example/model-v1","workersPerVM":2}
```

Changing the parent resource updates the VM template and
`status.declaredWorkerCapacity`. It does not rerun cloud-init in the existing
guest, restart that guest automatically, or send configuration to a running
inference process. There is no guest configuration update agent in this branch.

The observed update sequence was:

| Step | VM template / declared capacity | File inside running guest |
| --- | --- | --- |
| Initial boot | `example/model-v1`, 2 workers | `example/model-v1`, 2 workers |
| Parent updated | `example/model-v2`, 3 workers | Still `example/model-v1`, 2 workers |
| Explicit KubeVirt restart | `example/model-v2`, 3 workers | `example/model-v2`, 3 workers |

The VM and VMI identities stayed unchanged during the configuration update.
After `virtctl restart`, the VMI had a new identity. The test waited for
readiness and guest cloud-init completion, then read the file through the
guest agent and confirmed the new values.

For an existing test VM, the commands used to request that update and restart
were equivalent to:

```bash
# Select and verify the intended isolated test cluster first.
kubectl config current-context
kubectl -n issue23-test patch virtualinferencecluster demo-vm \
  --type=merge \
  -p '{"spec":{"model":"example/model-v2","workersPerVM":3}}'
virtctl -n issue23-test restart demo-vm-vm
```

These commands require a running test environment. The validation cluster was
removed after testing. A restart interrupts the guest and recreates its
temporary disk. Restarting from inside the guest is not the replacement
operation validated here.

This distinction matters because accepting a desired configuration does not
mean the running guest has applied it. Vetch reports pending when the current
VM generation has not been observed by its running instance. Declared capacity
still represents the request rather than observed inference capacity. A future
runtime integration would need to define how configuration is delivered and
when workers have applied it.

## Validation environment and results

The test used a dedicated Kind cluster named `vetch-issue23-test` and a separate
temporary kubeconfig. It did not modify an existing cluster.

| Component | Tested configuration |
| --- | --- |
| Host | NixOS x86-64 Linux with Intel KVM enabled |
| Docker | 29.8.1 |
| Kubernetes | Kind with `kindest/node:v1.35.0` |
| KubeVirt | Operator and deployment v1.9.0, matching the Go API dependency |
| Guest | Fedora Linux 44 from the image recorded above |

KubeVirt was installed using its
[official Kind guide](https://kubevirt.io/quickstart_kind/), with the release
pinned to v1.9.0. `/dev/kvm` was visible on the host and inside the Kind node;
KubeVirt advertised hardware KVM capacity. This session initially lacked its
configured Docker group, so Docker operations used `sg docker`.

Repository verification commands:

```bash
nix develop --command make test
nix develop --command make lint build
```

Both commands passed. Lint reported zero issues, controller test coverage was
78.5%, and generation left no tracked changes.

Real-cluster checks also passed:

- The sample created one owned VM and VMI and reached `VMReady`.
- Fedora booted with the requested resources, and guest cloud-init wrote the
  requested model and worker configuration.
- Over a ten-second stable observation, reconciliation preserved VM generation
  and VM/VMI identities. The owned VM watch repaired a changed managed label.
- Model and worker updates changed the existing VM template and declared
  capacity. An explicit restart delivered those settings inside the guest.
- Deleting the owned VM caused Vetch to recreate it and reach readiness.
- `vmCount: 0` removed both VM and VMI and reported `NoVMsRequested` with zero
  declared capacity.
- Switching to legacy mode removed the VM/VMI and created two placeholders;
  switching back removed those placeholders and created a ready VM.
- Parent deletion garbage-collected the VM, VMI, and launcher pod.
- Real Kubernetes API validation rejected `vmCount: 2`.
- A colliding, unrelated VM was not adopted or modified. Zero count and parent
  deletion also preserved that unrelated VM.
- CPU, memory, and image changes updated the VM template without replacing
  its identity. An unapplied generation was reported pending.

The controller initially used test-cluster credentials. Later creation,
deletion, mode-transition, and sizing checks ran with a dedicated service
account bound to the generated `config/rbac/role.yaml` ClusterRole.

Before removing the cluster, no test VM, VMI, VirtualInferenceCluster, or
launcher pod remained. The local controller was stopped, the dedicated cluster
was deleted, and temporary credentials were removed.

## What these results establish

The first single-VM lifecycle works on the tested Linux/KVM environment. The
validation does not establish inference serving, SSH access, persistent storage,
migration, or compatibility with every Kubernetes/KubeVirt version. CPU, memory,
and image updates were verified in the VM template; all their new values were
not separately checked inside a restarted sizing-test guest. The model/worker
restart test did verify the updated file inside the actual guest.
