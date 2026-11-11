# Vetch inference guest image

The KubeVirt guest image is a NixOS qcow2 containing the Vetch agent from
`cmd/agent`. It is separate from the controller container built by the root
`Dockerfile`. The guest starts the agent as the non-root `vetch` system user
after cloud-init supplies `/etc/vetch/config.json`. The agent currently serves
only process health (`GET /healthz`) and build version (`GET /version`); it does
not run an inference model or manage workers.

The NixOS image uses the KubeVirt image module in the repository's locked
Nixpkgs input. The lock file pins Nixpkgs and the agent's Go dependency hash
pins its module download. These inputs make a rebuild traceable; the disk and
OCI image still need build and boot validation.

## Build the disk

Use an x86-64 Linux machine with Nix flakes enabled, a working Nix builder,
Docker or a compatible container tool, and enough free space for the NixOS
closure and qcow2. Enter the repository's development shell, which provides
`qemu-img` and `jq` on Linux:

```bash
nix develop
make guest-qcow2
qemu-img info bin/guest-qcow2/vetch-guest.qcow2
```

`make guest-qcow2` runs `nix build .#guest-qcow2` and places only a symlink in
ignored `bin/`. The actual image is in the Nix store. The flake also exposes
`packages.x86_64-linux.vetch-agent` and `nixosConfigurations.vetch-guest` for
inspection. The Apple Silicon macOS shell can build the native agent package
and evaluate the guest configuration, but it is not a Linux KVM boot environment.

## Package and distribute the containerDisk

Choose an explicit tag in a registry reachable by the Kubernetes nodes.
Building the OCI image locally does not make it available to other nodes.

```bash
make guest-containerdisk GUEST_IMG=registry.example/vetch-guest:VERSION
docker push registry.example/vetch-guest:VERSION
```

The packaging script checks that the input is qcow2, copies it to ignored
`bin/guest-containerdisk/`, and builds `guest/Containerfile` for `linux/amd64`.
That container image contains the disk at `/disk/disk.qcow2`, owned by UID 107
so KubeVirt's qemu process can read it. No container entry point runs inside
this OCI image; KubeVirt boots the disk inside a VM. For a different container
tool, set `CONTAINER_TOOL` on the `make` invocation.

Record the pushed image digest and use it for `spec.vm.guestImage` when a stable
reference is needed. The sample continues to use a generic Fedora image until
the NixOS boot path is verified and an accessible Vetch image exists.

## Boot and validate

Use an isolated x86-64 Linux cluster with KVM and KubeVirt installed. Confirm
the Kubernetes context before changing any cluster resources. Start or restart
the Vetch controller after KubeVirt is installed so the controller registers
its VM watch. Copy the VM sample into ignored `bin/`, then set
`spec.vm.guestImage` in the copy to the pushed Vetch image reference.

```bash
kubectl config current-context
kubectl get nodes
cp config/samples/infrastructure_v1alpha1_virtualinferencecluster_vm.yaml bin/vetch-guest-vm.yaml
# Edit spec.vm.guestImage in bin/vetch-guest-vm.yaml to the pushed image.
kubectl apply -f bin/vetch-guest-vm.yaml
kubectl get vm demo-vm-vm -o yaml
kubectl get vmi
virtctl console demo-vm-vm
```

Use `virtctl port-forward` to reach guest port 8080 from the test machine;
check the exact syntax for the installed `virtctl` version with
`virtctl port-forward --help`. Then request `/healthz` and `/version` through
the forwarded local port. Those responses show that the agent is serving, not
that a model or inference worker is ready. Inspect cloud-init completion and
the agent service through the guest console or a separate test-access method.
The production image intentionally has no default password or SSH key; do not
put credentials in the image or source tree.

The boot record should show that cloud-init wrote the expected JSON, the
service started after cloud-init, and the running process has user `vetch`.
Check `systemctl status vetch-agent`, `journalctl -u vetch-agent`, and
`id vetch` from authenticated guest access in the isolated test setup. Also
check clean restart and that invalid configuration causes a visible service
failure rather than a false health response. No such KubeVirt boot evidence has
yet been recorded for this image.

The `containerDisk` is ephemeral: replacing the VM instance loses guest-side
changes. The controller currently provides only bootstrap config, so changing
`model` or `workersPerVM` in the resource requires a VM restart to apply it in
the guest. The current config file is written with mode `0644` and contains no
secrets; a future credential design must revise that permission.

## Verification recorded during implementation

The following repository-only checks passed on Apple Silicon macOS:

```bash
nix build path:.#packages.aarch64-darwin.vetch-agent --no-link
nix flake check --all-systems --no-build path:.
go test ./internal/agent ./cmd/agent
bash -n guest/build-containerdisk.sh
make -n guest-containerdisk GUEST_IMG=registry.example/vetch-guest:test
git diff --check
```

The NixOS configuration also evaluated with `cloud-init` enabled, NoCloud as
its data source, GRUB enabled, SSH disabled, and an agent service running as
`vetch` after `cloud-final.service`. The x86-64 Linux qcow2 and OCI image have
not been built here: this Mac has no Linux Nix builder configured and its Docker
daemon is unavailable. The KubeVirt boot checks above remain required before
issue #21 can be considered complete.
