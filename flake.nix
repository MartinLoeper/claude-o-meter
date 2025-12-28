{
  description = "A CLI tool that extracts Claude usage metrics as JSON";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    let
      # System-agnostic outputs
      systemAgnostic = {
        homeManagerModules.default = import ./nix/hm-module.nix;
        homeManagerModules.claude-o-meter = import ./nix/hm-module.nix;
      };

      # Per-system outputs
      perSystem = flake-utils.lib.eachDefaultSystem (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          packages = {
            default = pkgs.buildGoModule {
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
          };

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
