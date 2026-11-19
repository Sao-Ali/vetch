{ pkgs, lib, vetchAgent, ... }:

let
  model = pkgs.fetchurl {
    url = "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/23749fefcc72300e3a2ad315e1317431b06b590a/Qwen3-0.6B-Q8_0.gguf";
    sha256 = "9465e63a22add5354d9bb4b99e90117043c7124007664907259bd16d043bb031";
  };
in

{
  system.stateVersion = "26.05";
  image.baseName = "vetch-guest";

  boot.loader.grub.enable = true;
  networking.useDHCP = lib.mkDefault true;
  # The controller and test clients need to reach the agent's HTTP listener.
  networking.firewall.allowedTCPPorts = [ 8080 ];
  services.cloud-init.settings.datasource_list = [ "NoCloud" ];

  # The agent has no SSH dependency; access for boot checks uses KubeVirt's console.
  services.openssh.enable = lib.mkForce false;

  users.groups.vetch = { };
  users.users.vetch = {
    isSystemUser = true;
    group = "vetch";
  };

  # Copy to a regular, immutable file; the agent rejects symlink artifacts.
  system.activationScripts.vetch-model.text = ''
    install -d -m 0755 /var/lib/vetch/models
    install -m 0444 ${model} /var/lib/vetch/models/Qwen3-0.6B-Q8_0.gguf
  '';

  systemd.services.vetch-agent = {
    description = "Vetch VM agent";
    wantedBy = [ "multi-user.target" ];
    requires = [ "cloud-final.service" ];
    after = [ "cloud-final.service" ];
    environment = {
      VETCH_RUNTIME = "${pkgs.llama-cpp}/bin/llama-server";
      VETCH_MODEL_ROOT = "/var/lib/vetch/models";
      VETCH_CGROUP_ROOT = "/sys/fs/cgroup/system.slice/vetch-agent.service";
    };
    serviceConfig = {
      User = "vetch";
      Group = "vetch";
      ExecStartPre = "${pkgs.coreutils}/bin/test -r /etc/vetch/config.json";
      ExecStart = "${vetchAgent}/bin/agent";
      Restart = "on-failure";
      RestartSec = 2;
      NoNewPrivileges = true;
      ProtectSystem = "strict";
      ProtectHome = true;
      PrivateTmp = true;
      Delegate = "cpu memory";
      DelegateSubgroup = "supervisor";
      KillMode = "control-group";
    };
  };
}
