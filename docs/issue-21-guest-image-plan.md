# Issue #21: first inference guest image

This records the design and validation plan. The NixOS image build files are
implemented; a KubeVirt boot and agent-in-guest validation are still pending.
Issue: https://github.com/Sao-Ali/vetch/issues/21

## Current handoff

The Kubernetes controller creates one KubeVirt VM in VM mode and sets its
`containerDisk` image from `spec.vm.guestImage`. It supplies `model` and
`workersPerVM` as `/etc/vetch/config.json` through cloud-init NoCloud. The
sample currently uses a generic Fedora disk image, which does not contain the
Vetch agent.

Issue #17 added a separate Go agent (`cmd/agent`). It reads that JSON file,
validates it, and serves `GET /healthz` and `GET /version` on port 8080 by
default. It does not yet run models, manage workers, or contact Kubernetes.
The controller executable (`cmd/main.go`) runs outside the guest. VM readiness
from KubeVirt and process health from the agent are distinct observations;
neither proves that inference is serving.

## Decision and scope

Try a NixOS-built guest image first. Use the repository's locked Nixpkgs input
to declare the guest OS, agent package, non-root `vetch` account, cloud-init,
networking, and systemd service. Package the resulting bootable qcow2 disk as a
KubeVirt `containerDisk`. A successful Nix build is insufficient: booting the
image through the existing Vetch VM resource is the deciding integration test.
Keep the Fedora sample usable until the NixOS image passes and has an image
reference reachable by the test cluster.

Issue #21 ends at a booted VM with the existing agent running from the
controller-provided configuration and documented rebuild steps. It does not
add a model runtime, inference workers, a gateway, a CLI, edge scheduling,
device provisioning, or `edge-nix` integration. The Kubernetes API remains
Vetch's control-plane API. Do not change the controller's guest contract or
status meaning merely to package the image.

## Implementation sequence and change boundaries

1. Add an `x86_64-linux` Nix output that packages `./cmd/agent` with the
   existing Go module inputs. Keep its JSON format and HTTP endpoints intact.
   This isolates packaging from agent behavior and the controller binary.
2. Define the smallest NixOS guest needed to boot under KubeVirt: kernel and
   bootloader, network configuration, cloud-init NoCloud, the agent package,
   a system `vetch` user, and a systemd unit. Have the unit wait for cloud-init
   to write `/etc/vetch/config.json`, start as `vetch`, and restart on failure.
   Verify the actual file permissions and ordering in the guest. The current
   config contains only a model identifier and worker count; do not assume
   its present `0644` mode is suitable for future secrets.
3. Build a standalone bootable qcow2 from that NixOS configuration. Record
   its architecture and inspect the disk format. A NixOS VM test or a QEMU
   boot is useful early feedback but cannot replace a KubeVirt boot check.
4. Add a dedicated `containerDisk` packaging path with the disk under `/disk`
   and readable by KubeVirt's qemu UID 107. Keep the controller Dockerfile
   unchanged. Use an explicit image tag or digest and a registry reachable
   from the test cluster; a locally built image alone is not enough.
5. In an isolated Linux KVM cluster, set `spec.vm.guestImage` to the built
   image and let the existing controller create the VM. Check VM boot,
   cloud-init completion, the config file, `systemctl` and journal output,
   the agent process UID, and HTTP responses from `/healthz` and `/version`.
   Also test a clean restart and a deliberately invalid config so service
   failure is visible. Do not interpret agent health as worker readiness.
6. Document exact Linux build prerequisites, Nix build commands, disk and OCI
   outputs, registry availability, guest image selection, boot inspection,
   rebuild procedure, and known limits. If Go code changes, run
   `make lint-fix` and `make test`; if API markers change, run
   `make manifests generate`. Issue #21 should not require API changes.

If the NixOS disk cannot satisfy the boot and cloud-init checks, retain the
same guest contract and use a pinned, checksum-verified Fedora cloud qcow2
customized with the agent and service. Record the failure before switching.

## Future architecture, outside issue #21

The proposed direction is a local entry point that routes inference to a VM
on a capable Linux/KVM host or to native processes on edge devices. The local
host may use Kubernetes and KubeVirt; an UNO Q or Jetson need not host either.
Edge devices should initially keep their supported Linux and hardware stacks.
One device may run one or several requests or workers depending on measured
memory, CPU/GPU use, and runtime behavior; multiple VMs are not a prerequisite.

When the first inference worker is implemented, evaluate one portable agent
core with platform-specific execution backends and a common health, capacity,
and inference contract. Decide this using VM and physical-device evidence.
`edge-nix` can later supply validated ONNX artifacts, especially for its
currently tested UNO Q CPU path. A shared VM/edge workload build is a future
integration, not a capability of the current repositories. A local gateway is
an inference data-plane proposal; the Kubernetes API remains the current
control plane. AWS deployment is not a requirement for issue #21.

## Evidence required for completion

- Rebuildable source and pinned build inputs for the agent, NixOS qcow2, and
  `containerDisk`, with no generated disk or credentials committed.
- A KubeVirt VM boot record from an isolated Linux KVM environment.
- Evidence that cloud-init supplied the expected JSON and the agent runs as
  non-root `vetch` after it is available.
- Successful agent health and version requests, plus clear failure on invalid
  configuration; no claim of inference readiness.
- Rebuild and validation commands in tracked documentation.

The current Apple Silicon macOS development host and its Docker Desktop kind
setup are not a verified KubeVirt hardware-virtualization test environment.
