{
  description = "A CLI tool that extracts Claude usage metrics as JSON";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    claude-code = {
      url = "github:sadjow/claude-code-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, claude-code }:
    let
      # Version follows claude-code versioning
      version = "2.0.76";

      # Build package for a given system
      mkPackage = pkgs: pkgs.buildGoModule {
        pname = "claude-o-meter";
        inherit version;

        src = ./.;

        vendorHash = null;

        ldflags = [
          "-s" "-w"
          "-X main.Version=${version}"
        ];

        meta = with pkgs.lib; {
          description = "A CLI tool that extracts Claude usage metrics as JSON";
          homepage = "https://github.com/MartinLoeper/claude-o-meter";
          license = licenses.mit;
          maintainers = [ ];
          mainProgram = "claude-o-meter";
        };
      };

      # System-agnostic outputs
      systemAgnostic = {
        homeManagerModules.default = { config, lib, pkgs, ... }:
          let
            system = pkgs.stdenv.hostPlatform.system;
            # Import the actual module and pass the default package and claude-code
            module = import ./nix/hm-module.nix {
              defaultPackage = mkPackage pkgs;
              claudeCodePackage = claude-code.packages.${system}.default;
            };
          in
          module { inherit config lib pkgs; };

        homeManagerModules.claude-o-meter = self.homeManagerModules.default;
      };

      # Per-system outputs
      perSystem = flake-utils.lib.eachDefaultSystem (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          packages.default = mkPackage pkgs;

          devShells.default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go
              gopls
              gotools
            ];
          };
        }
      );
    in
    systemAgnostic // perSystem;
}
