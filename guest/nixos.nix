{ pkgs, lib, vetchAgent, ... }:

{
  system.stateVersion = "26.05";
  image.baseName = "vetch-guest";

  boot.loader.grub.enable = true;
  networking.useDHCP = lib.mkDefault true;
  services.cloud-init.settings.datasource_list = [ "NoCloud" ];

  # The agent has no SSH dependency; access for boot checks uses KubeVirt's console.
  services.openssh.enable = lib.mkForce false;

  users.groups.vetch = { };
  users.users.vetch = {
    isSystemUser = true;
    group = "vetch";
  };

  systemd.services.vetch-agent = {
    description = "Vetch VM agent";
    wantedBy = [ "multi-user.target" ];
    requires = [ "cloud-final.service" ];
    after = [ "cloud-final.service" ];
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
    };
  };
}
