{
  description = "Vetch development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs, ... }:
    let
      supportedSystems = [
        "aarch64-darwin"
        "x86_64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
      pkgsFor = system: import nixpkgs { inherit system; };
      agentPackage = system:
        let pkgs = pkgsFor system;
        in pkgs.buildGoModule {
          pname = "vetch-agent";
          version = "0.1.0";
          src = self;
          subPackages = [ "cmd/agent" ];
          go = pkgs.go_1_26;
          vendorHash = "sha256-azHCwq4x+lWgbi0FUGVoa5DU/P3tF0hmTCG2zZ5q/kQ=";
          env.CGO_ENABLED = "0";
        };
    in
    {
      packages = forAllSystems (system:
        { vetch-agent = agentPackage system; }
        // nixpkgs.lib.optionalAttrs (system == "x86_64-linux") {
          guest-qcow2 = self.nixosConfigurations.vetch-guest.config.system.build.kubevirtImage;
        }
      );

      nixosConfigurations.vetch-guest = nixpkgs.lib.nixosSystem {
        system = "x86_64-linux";
        specialArgs.vetchAgent = self.packages.x86_64-linux.vetch-agent;
        modules = [
          "${nixpkgs}/nixos/modules/virtualisation/kubevirt.nix"
          ./guest/nixos.nix
        ];
      };

      devShells = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.mkShell {
            packages = (with pkgs; [
              git
              go_1_26
              gopls
              gnumake
              kind
              kubectl
              kustomize
            ]) ++ nixpkgs.lib.optionals pkgs.stdenv.hostPlatform.isLinux (with pkgs; [ jq qemu ]);

            GOTOOLCHAIN = "local";
          };
        }
      );
    };
}
