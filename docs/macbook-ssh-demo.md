# Connect a MacBook to the Vetch server

Connect to the existing NixOS host `nixos-dev` as `ali`. This is the Linux
machine running Vetch, not an inference guest VM. No new Linux server is
needed. The MacBook sends commands and prompts; Kubernetes, model storage,
and inference execution stay on this machine.

## This machine's connection details

Observed on 2026-11-19; recheck addresses after network or Tailscale changes.

| Setting | Value |
| --- | --- |
| Linux distribution | NixOS |
| Hostname | `nixos-dev` |
| SSH username | `ali` |
| SSH port | `22` |
| Preferred private address | `100.107.229.23` (Tailscale) |
| Tailscale DNS name | `nixos-dev.tail0ea671.ts.net` |
| Current Wi-Fi address | `192.168.1.140` (DHCP; may change) |
| Repository | `/home/ali/vetch` |
| SSH daemon | Active and enabled |
| Login method | SSH public key; password and root login disabled |
| Authorized client keys | Neither configured key file existed during inspection; Mac key enrollment is still needed |

The host's ED25519 **public host-key fingerprint** is:

```text
SHA256:cE6I6XE6MDYb8pkhsa24i1kp2BoP9uZwV7C/D3bXMLg
```

The NixOS configuration imports `/etc/nixos/remote-access.nix`, which enables
OpenSSH and Tailscale, allows SSH user `ali`, and opens TCP 22 on the Tailscale
interface. It does not open SSH on Wi-Fi. Use the Tailscale address even when
both computers are on the same Wi-Fi network. No router port forwarding is
needed for this setup.

Tailscale is running, and its Tailscale SSH feature is disabled. This guide
uses ordinary OpenSSH with your Mac's key over the Tailscale network.

## 1. Prepare the MacBook

Install and connect Tailscale using the
[official macOS installation instructions](https://tailscale.com/docs/install/mac).
Join the tailnet containing `nixos-dev`; its access policy must permit your Mac
to reach that machine on TCP 22. Confirm the server appears online in Tailscale.

Open Terminal on the Mac. SSH and curl are sufficient for the demo; Docker,
Nix, Kubernetes tools, and the server's kubeconfig are not needed on the Mac.
Check the SSH network path:

```bash
nc -vz 100.107.229.23 22
```

Success means TCP 22 is reachable; it does not yet prove key authentication.

Create a dedicated SSH key if that filename does not already exist. Keep an
existing key rather than overwriting it, and choose a passphrase when prompted:

```bash
mkdir -p "$HOME/.ssh"
chmod 700 "$HOME/.ssh"
if test ! -e "$HOME/.ssh/id_ed25519_vetch" && test ! -e "$HOME/.ssh/id_ed25519_vetch.pub"; then
  ssh-keygen -t ed25519 -f "$HOME/.ssh/id_ed25519_vetch" -C "vetch-macbook"
fi
pbcopy < "$HOME/.ssh/id_ed25519_vetch.pub"
```

Only the `.pub` file goes to Linux. The file without `.pub` is the private key
and stays on the Mac. If only one of these files already exists, resolve that
key pair before continuing rather than generating over it.

## 2. Enroll the Mac's public key on Linux once

Because password login is disabled and no authorized-key file was found,
the first enrollment needs the Linux machine's local console or an existing
trusted administrative session. A first-time SSH login cannot install its own
key, and `ssh-copy-id` cannot bypass this requirement.

At that Linux console, logged in as `ali`, run:

```bash
test "$(id -un)" = ali || exit 1
install -d -m 700 /home/ali/.ssh
touch /home/ali/.ssh/authorized_keys
chmod 600 /home/ali/.ssh/authorized_keys
cat >> /home/ali/.ssh/authorized_keys
```

Paste the full public-key line copied from the Mac, press Enter, then Ctrl-D
to finish. This appends the key and preserves existing entries. No SSH restart
or NixOS rebuild is needed for this home-directory key file. Do not paste a
private key or commit either key file to the repository.

## 3. Save a connection alias on the Mac

Add this block to the Mac's `~/.ssh/config`, preserving existing entries. If a
`Host vetch-linux` block already exists, update that block instead of duplicating it.

```sshconfig
Host vetch-linux
    HostName 100.107.229.23
    User ali
    Port 22
    IdentityFile ~/.ssh/id_ed25519_vetch
    IdentitiesOnly yes
    HostKeyAlgorithms ssh-ed25519
    ServerAliveInterval 30
    ServerAliveCountMax 3
    ForwardAgent no
```

You can use `nixos-dev.tail0ea671.ts.net` for `HostName` when MagicDNS resolves
on the Mac. The numeric Tailscale address avoids that DNS dependency.

On the Mac:

```bash
chmod 600 "$HOME/.ssh/config"
ssh vetch-linux
```

At the first connection, compare the ED25519 fingerprint against the value
above before accepting it. After login, these commands should report `ali`,
`nixos-dev`, and `/home/ali/vetch`, respectively:

```bash
whoami
hostname
cd /home/ali/vetch
pwd
```

Without the alias, the equivalent connection is:

```bash
ssh -o IdentitiesOnly=yes -o HostKeyAlgorithms=ssh-ed25519 \
  -i "$HOME/.ssh/id_ed25519_vetch" ali@100.107.229.23
```

## 4. Run the inference demo from the Mac

There is currently no demo cluster left running from validation. First use
[the dedicated integration deployment](local-inference.md#dedicated-integration-deployment)
on Linux to build/load the current images, create `vetch-inference-test`, and
wait for `Available=True`. SSH access alone does not provision the VM.

Then follow [the MacBook demo commands](edge-device-testing-plan.md#1-demonstrate-central-inference-from-the-macbook).
They use three Mac Terminal windows:

1. `ssh vetch-linux`, then run the server-side Kubernetes Service port-forward
   on Linux loopback `127.0.0.1:8082`.
2. Keep this SSH tunnel running on the Mac:

   ```bash
   ssh -N -o ExitOnForwardFailure=yes \
     -L 127.0.0.1:18082:127.0.0.1:8082 vetch-linux
   ```

3. Run the documented macOS curl request against `http://127.0.0.1:18082/infer`.
   A successful response has HTTP 200, generated text, model/backend/worker
   identity, token counts, and timing information.

The path is Mac curl -> SSH tunnel -> Linux Service port-forward -> gateway
-> VM agent -> llama.cpp. Both forwarded listeners bind to loopback. Keep both
forwarding sessions open; recreate them after a disconnect. Controller,
gateway, and VM workloads run in Kubernetes independently of those sessions.
The guest VM itself has SSH disabled; use the host-side inspection commands
in the demo evidence checklist to demonstrate where inference runs.

## Troubleshooting

| Symptom | Check or action |
| --- | --- |
| TCP 22 times out | Confirm the host is awake, both Tailscale clients are connected, the server is online, and tailnet policy permits TCP 22. Use `100.107.229.23`, not the Wi-Fi address. |
| `Permission denied (publickey)` | Use username `ali`, check the Mac's `IdentityFile`, and confirm its matching public key was appended on Linux. Password authentication is disabled. |
| DNS name does not resolve | Use `100.107.229.23` in the SSH alias and check MagicDNS separately. |
| Host-key mismatch | Verify the current public fingerprint at the Linux console with `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`. Investigate the change before updating the Mac's known-host entry. |
| Local port `18082` already in use | Choose a free Mac port in `-L`, and use that same port in curl. |
| Tunnel says connection refused to port `8082` | Start or restore the Linux-side `kubectl port-forward`; check the dedicated cluster and gateway are running. |
| Forwarding is administratively prohibited | Check the host's OpenSSH forwarding policy with its administrator. |
| kubectl cannot connect or has no context | Set the dedicated kubeconfig on Linux and follow the cluster setup guide. Do not copy credentials to the Mac to work around it. |
| HTTP 503 from `/infer` | Check VM/worker readiness and whether workers are busy; a working SSH connection does not imply available inference capacity. |
| Request deadline error | Generate a fresh deadline with the documented macOS command and check both machines' clocks. |

For key-selection diagnostics, run `ssh -vv vetch-linux` on the Mac and inspect
which identity is offered. Keep diagnostic output private when it contains
account or network details.

## What has been checked

Read-only inspection confirmed the host/user, addresses, active SSH and
Tailscale services, SSH configuration, public host-key fingerprint, and missing
authorized-key files. A server-local SSH banner check succeeded; it does not
prove inbound connectivity from the Mac. No MacBook connection or key enrollment
has been performed by this documentation change, and no host SSH/firewall
configuration was changed. The acceptance test is a real Mac login followed by
an HTTP 200 inference response through the tunnel.
