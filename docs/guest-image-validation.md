# Issue 21 NixOS guest validation

Date: 2026-11-17 (America/Los_Angeles).
Repository: `/home/ali/vetch`.
Branch: `issue-21-nixos-guest`.
Commit: `359a449cbf4eabe64c0fd58ac3db88eab242a527`.

Result: the unchanged branch builds and boots successfully, but does not pass
the complete network-access workflow in `docs/guest-image.md`. The guest
firewall blocks inbound agent traffic. The documented virtctl forwarding path
also timed out with the guest firewall stopped, so opening the firewall alone
has not been demonstrated to fix that path. The validation run made no tracked
source changes.

## Environment and isolation

- NixOS, x86-64 Linux, `/dev/kvm` available; Docker 29.8.1.
- Repository-pinned Nixpkgs and Go 1.26.7 through `nix develop`.
- Dedicated Kind cluster `vetch-issue21-test`, Kubernetes v1.35.0.
- KubeVirt and virtctl v1.9.0, matching the repository's KubeVirt API dependency.
- Dedicated namespace `issue21-test` and separate ignored kubeconfig.
- The unmodified branch controller ran locally against that cluster only.
- No existing Kubernetes context was configured; no existing cluster was used.

KubeVirt installation followed the [official Kind quickstart](https://kubevirt.io/quickstart_kind/)
with the release pinned to v1.9.0. Guest inspection used authenticated Kubernetes
admin access to the launcher and QEMU guest-agent commands; no guest password or
SSH key was added. Guest commands used `virsh qemu-agent-command` with
`guest-exec` and `guest-exec-status` through the launcher compute container.

## Results

The evidence filenames below refer to raw artifacts retained locally in
`/home/ali/vetch-issue21-test-results/` on the validation host. Those artifacts
are not committed; this report records their results, relevant outputs, and
commands.

| Check | Result | Evidence |
| --- | --- | --- |
| Agent Go tests | PASS | `repository-checks.log` |
| Full Go/envtest suite | PASS; controller coverage 78.5%, agent coverage 83.0% | `full-repository-checks.log` |
| Lint and controller build | PASS; zero lint issues | `full-repository-checks.log` |
| Flake evaluation across supported systems | PASS; evaluation only | `flake-check.log` |
| Packaging shell syntax and Make dry run | PASS | `repository-checks.log` |
| Packaging rejects missing arguments, missing disk, and invalid format | PASS | `packaging-*.log` |
| Linux agent and qcow2 build | PASS | `qcow2-build.log` |
| qcow2 format inspection | PASS; virtual size 4,775,215,104 bytes, actual size 2,484,944,896 bytes; no backing file | `disk-info.json` |
| ContainerDisk build for linux/amd64 | PASS | `container-build.log`, `container-image-inspect.json` |
| Disk accessible to qemu UID 107 | PASS; mounted backing disk reported UID:GID 107:107 | Runtime `stat`; successful VM boot |
| Controller-created KubeVirt VM boot | PASS; NixOS 26.11 and KVM domain | `guest-initial.log`, `domain.xml` |
| DHCP and guest network address | PASS | `guest-network.log` |
| Cloud-init NoCloud configuration | PASS; expected JSON, mode 0644 root:root, no cloud-init errors | `guest-ready.log` |
| Agent starts after cloud-final | PASS; monotonic activation timestamps confirm ordering | `guest-ready.log`, `restart-verification.log` |
| Agent process runs as vetch | PASS; UID 996 and GID 994 | `guest-version-and-user.log`, `guest-after-restart.log` |
| Guest-local health and version | PASS; HTTP 200, `{"status":"ok"}` and `{"version":"dev"}` | `guest-ready.log`, `guest-version-and-user.log` |
| Inbound agent access with default firewall | FAIL; node-to-guest request times out | `node-health-firewall-on.log`, `nix-settings.json` |
| Inbound access with guest firewall temporarily stopped | PASS; both endpoints return HTTP 200 | `node-health-firewall-stopped.log` |
| Documented virtctl port-forward workflow | FAIL; requests time out, including fresh forwarding with firewall stopped | `health-forward.error`, `firewall-isolation*.log`, `port-forward*.log` |
| Invalid config | PASS; status 1, clear error, restart attempts, no listening health endpoint | `invalid-config.log`, `test-invalid-config.sh` |
| Recovery after restoring config | PASS | `invalid-config.log` |
| VM restart | PASS; new VMI UID, same config, successful cloud-init, non-root agent and HTTP 200 again | `restart-verification.log`, `guest-after-restart.log`, `vmi-final.json` |
| Cleanup | PASS; parent deletion removed VM/VMI/pod, dedicated cluster removed | `resource-cleanup.log`, `cluster-cleanup.log` |

Expected controller-provided guest JSON:

```json
{"model":"example/model-v1","workersPerVM":2}
```

## Commands and image identity

Repository checks were run from this branch:

```bash
nix develop -c bash -c 'go test ./internal/agent ./cmd/agent'
bash -n guest/build-containerdisk.sh
nix flake check --all-systems --no-build
nix develop -c bash -c 'make test && make lint build'
git diff --check
nix build .#guest-qcow2 --out-link bin/guest-qcow2
nix develop -c qemu-img info --output=json bin/guest-qcow2/vetch-guest.qcow2
nix develop -c make guest-containerdisk GUEST_IMG=vetch-guest:issue21-test
```

The built OCI image index digest was
`sha256:4548dd942eda930e4ed27ad20343b783b2e3517c3a369b4dc416bf7b7bd29c69`.
The linux/amd64 manifest digest was
`sha256:6faf2a0127758aaa6665d8c7dd8997bcb84d261d6f8690e965a8dc99d2d5624a`.
These identify this local build; no registry publication occurred.

The image was streamed into the dedicated node to avoid an extra multi-gigabyte
archive on the host:

```bash
docker save vetch-guest:issue21-test |
  docker exec -i vetch-issue21-test-control-plane \
    ctr --namespace k8s.io images import --all-platforms -
```

The VM sample was copied into ignored `bin/issue21-validation/test-vm.yaml`,
changed only to select `vetch-guest:issue21-test`, and applied in the dedicated
namespace. Forwarding attempts used virtctl v1.9.0 with both VM and VMI targets,
local ports 18080/18082, and guest port 8080. Both listeners started, but requests
timed out. The separate direct node-to-guest tests isolated the firewall issue.
Do not infer that the firewall is the sole cause of the forwarding failure.

## Remaining work and retained artifacts

The guest needs an explicit policy allowing inbound TCP 8080 for its current
agent contract. A likely configuration change is
`networking.firewall.allowedTCPPorts = [ 8080 ];`; this source change and its
rebuilt image have not been tested in this run. The documented port-forwarding
path needs separate diagnosis or a verified alternative access procedure.

Registry push, registry-based pulls, an Apple Silicon package build, and bitwise
reproducibility across independent builds were not tested. The current agent
does not implement inference; these results make no claim about model serving.

The built disk remains at
`/home/ali/vetch/bin/guest-qcow2/vetch-guest.qcow2` (a Nix store output).
Temporary Docker build context/cache, the host's test image, forwarding
processes, local controller, and dedicated Kind cluster were removed. No
kubeconfig or credential values are included in this report directory.
At the end of validation, the repository was clean on `issue-21-nixos-guest`.
