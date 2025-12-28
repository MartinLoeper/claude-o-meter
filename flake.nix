{
  description = "A CLI tool that extracts Claude usage metrics as JSON";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    let
      # Build package for a given system
      mkPackage = pkgs: pkgs.buildGoModule {
        pname = "claude-o-meter";
        version = "0.1.0";

        src = ./.;

        vendorHash = null;

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
            # Import the actual module and pass the default package
            module = import ./nix/hm-module.nix {
              defaultPackage = mkPackage pkgs;
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
