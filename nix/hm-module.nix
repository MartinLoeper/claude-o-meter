{ defaultPackage }:

{ config, lib, pkgs, ... }:

let
  cfg = config.services.claude-o-meter;
in
{
  options.services.claude-o-meter = {
    enable = lib.mkEnableOption "claude-o-meter daemon for Claude usage metrics";

    package = lib.mkOption {
      type = lib.types.package;
      default = defaultPackage;
      defaultText = lib.literalExpression "pkgs.claude-o-meter";
      description = "The claude-o-meter package to use";
    };

    interval = lib.mkOption {
      type = lib.types.str;
      default = "60s";
      example = "30s";
      description = "How often to query Claude usage metrics";
    };

    outputFile = lib.mkOption {
      type = lib.types.str;
      default = "${config.xdg.cacheHome}/claude-o-meter.json";
      example = "/tmp/claude-usage.json";
      description = "Path where the JSON output will be written";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.user.services.claude-o-meter = {
      Unit = {
        Description = "Claude usage metrics daemon";
        After = [ "network.target" ];
      };

      Service = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/claude-o-meter daemon -i ${cfg.interval} -f ${cfg.outputFile}";
        Restart = "always";
        RestartSec = "10s";

        # Ensure the daemon has access to Claude CLI and required tools
        # - unbuffer (from expect) for PTY in headless environments
        # - script (from util-linux) as fallback
        # TERM is required for PTY to work properly
        # HOME is needed for claude CLI config access
        Environment = [
          "PATH=${config.home.profileDirectory}/bin:${pkgs.expect}/bin:${pkgs.util-linux}/bin:/usr/bin:/bin"
          "TERM=xterm-256color"
          "HOME=${config.home.homeDirectory}"
        ];
      };

      Install = {
        WantedBy = [ "default.target" ];
      };
    };
  };
}
