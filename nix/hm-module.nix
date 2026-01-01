{ defaultPackage, claudeCodePackage }:

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

    stateFile = lib.mkOption {
      type = lib.types.str;
      default = "${config.xdg.cacheHome}/claude-o-meter.json";
      example = "${config.xdg.cacheHome}/claude-usage.json";
      description = "Path where the daemon state will be written (defaults to XDG cache directory)";
    };

    debug = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Print claude CLI output in real-time to journalctl for debugging";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.user.services.claude-o-meter = {
      Unit = {
        Description = "Claude usage metrics daemon";
        After = [ "network.target" ];
        # Restart service when packages change
        X-Restart-Triggers = [
          cfg.package
          claudeCodePackage
        ];
      };

      Service = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/claude-o-meter daemon -i ${cfg.interval} -s ${cfg.stateFile}${lib.optionalString cfg.debug " --debug"}";
        Restart = "always";
        RestartSec = "10s";

        # Ensure the daemon has access to Claude CLI and required tools
        # - claude-code: the Claude CLI we depend on (pinned version)
        # - coreutils for mktemp, chmod, dirname, yes (needed by claude wrapper and prompts)
        # - procps for ps (needed by claude internally)
        # - bash for command piping
        # - unbuffer (from expect) for PTY in headless environments
        # - script (from util-linux) as fallback
        # TERM is required for PTY to work properly
        # HOME is needed for claude CLI config access
        Environment = [
          "PATH=${claudeCodePackage}/bin:${pkgs.coreutils}/bin:${pkgs.procps}/bin:${pkgs.bash}/bin:${pkgs.expect}/bin:${pkgs.util-linux}/bin:/usr/bin:/bin"
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
