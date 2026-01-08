{
  description = "A CLI tool that extracts Claude usage metrics as JSON";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    claude-code = {
      url = "github:sadjow/claude-code-nix?ref=v2.1.1";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, claude-code ? null }:
    let
      # Read version from VERSION file (single source of truth)
      version = builtins.replaceStrings [ "\n" ] [ "" ] (builtins.readFile ./VERSION);

      # Build package for a given system
      mkPackage = pkgs:
        let
          goPackage = pkgs.buildGoModule {
            pname = "claude-o-meter";
            inherit version;

            src = ./.;

            vendorHash = "sha256-7tiSwNhq6e4LEh4lUkfh2i4tEdWWL6TxQpYYwYKsfog=";

            nativeBuildInputs = with pkgs; [
              pkg-config
            ];

            buildInputs = with pkgs; [
              dbus
            ];

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
        in
        # Wrap the package to include the icon in share/claude-o-meter/
        pkgs.symlinkJoin {
          name = "claude-o-meter-${version}";
          paths = [ goPackage ];
          postBuild = ''
            mkdir -p $out/share/claude-o-meter
            cp ${./assets/claude-icon.png} $out/share/claude-o-meter/claude-icon.png
          '';
          meta = goPackage.meta;
        };

      # System-agnostic outputs
      systemAgnostic = {
        homeManagerModules.default = { config, lib, pkgs, ... }:
          let
            system = pkgs.stdenv.hostPlatform.system;
            # Import the actual module and pass the default package and claude-code
            # claude-code input is optional - pass null if not provided
            module = import ./nix/hm-module.nix {
              defaultPackage = mkPackage pkgs;
              claudeCodePackage =
                if claude-code != null
                then claude-code.packages.${system}.default
                else null;
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
              pkg-config
              dbus
            ];
          };
        }
      );
    in
    systemAgnostic // perSystem;
}
