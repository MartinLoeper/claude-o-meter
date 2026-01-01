{ defaultPackage, claudeCodePackage }:

{ config, lib, pkgs, ... }:

let
  cfg = config.services.claude-o-meter;

  # Build the Claude Code plugin package
  claudeCodePlugin = import ./claude-code-plugin.nix {
    inherit pkgs;
    claudeOMeterPackage = cfg.package;
  };

  # Build the marketplace package containing the plugin
  claudeCodeMarketplace = import ./claude-code-marketplace.nix {
    inherit pkgs claudeCodePlugin;
  };
in
{
  options.services.claude-o-meter = {
    enable = lib.mkEnableOption "claude-o-meter daemon for Claude usage metrics";

    enableClaudeCodeHooks = lib.mkEnableOption ''
      Claude Code integration via hooks.

      When enabled, installs a Claude Code plugin marketplace that automatically
      triggers a refresh when Claude conversations end. This allows using a longer
      polling interval (5 minutes instead of 1 minute) since metrics are
      updated in real-time via the stop hook.

      The marketplace is installed to ~/.claude/claude-o-meter-plugins/
    '';

    package = lib.mkOption {
      type = lib.types.package;
      default = defaultPackage;
      defaultText = lib.literalExpression "pkgs.claude-o-meter";
      description = "The claude-o-meter package to use.";
    };

    claudeCodePackage = lib.mkOption {
      type = lib.types.package;
      default =
        if claudeCodePackage != null
        then claudeCodePackage
        else throw ''
          services.claude-o-meter.claudeCodePackage must be set.

          The claude-code flake input was not provided. You need to either:
          1. Add the claude-code input to your flake and pass it to claude-o-meter
          2. Set services.claude-o-meter.claudeCodePackage to your own Claude Code package
        '';
      defaultText = lib.literalExpression "claude-code-nix";
      description = ''
        The Claude Code CLI package to use. Override this to use a different
        version or your own build of Claude Code.

        If the claude-code flake input is not provided, this option must be set explicitly.
      '';
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
      example = "/tmp/claude-usage.json";
      description = "Path where the JSON output will be written";
    };

    debug = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Print claude CLI output in real-time to journalctl for debugging";
    };

    enableDbus = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Enable D-Bus service for external refresh triggers (e.g., from Claude Code hooks)";
    };
  };

  config = lib.mkIf cfg.enable (lib.mkMerge [
    # Assertions
    {
      assertions = [
        {
          assertion = cfg.enableClaudeCodeHooks -> cfg.enableDbus;
          message = ''
            services.claude-o-meter.enableClaudeCodeHooks requires enableDbus to be true.
            The Claude Code hook uses 'claude-o-meter refresh' which communicates via D-Bus.
          '';
        }
      ];
    }

    # Base configuration
    {
      systemd.user.services.claude-o-meter = {
        Unit = {
          Description = "Claude usage metrics daemon";
          After = [ "network.target" ];
          # Restart service when packages change
          X-Restart-Triggers = [
            cfg.package
            cfg.claudeCodePackage
          ];
        };

        Service = {
          Type = "simple";
          ExecStartPre = "-${pkgs.coreutils}/bin/rm -f ${cfg.stateFile}";
          ExecStart = "${cfg.package}/bin/claude-o-meter daemon -i ${cfg.interval} -f ${cfg.stateFile}${lib.optionalString cfg.debug " --debug"}${lib.optionalString cfg.enableDbus " --dbus"}";
          Restart = "always";
          RestartSec = "10s";

          # Ensure the daemon has access to Claude CLI and required tools
          # - claude-code: the Claude CLI we depend on (configurable via claudeCodePackage option)
          # - coreutils for mktemp, chmod, dirname, yes (needed by claude wrapper and prompts)
          # - procps for ps (needed by claude internally)
          # - bash for command piping
          # - unbuffer (from expect) for PTY in headless environments
          # - script (from util-linux) as fallback
          # TERM is required for PTY to work properly
          # HOME is needed for claude CLI config access
          Environment = [
            "PATH=${cfg.claudeCodePackage}/bin:${pkgs.coreutils}/bin:${pkgs.procps}/bin:${pkgs.bash}/bin:${pkgs.expect}/bin:${pkgs.util-linux}/bin:/usr/bin:/bin"
            "TERM=xterm-256color"
            "HOME=${config.home.homeDirectory}"
          ];
        };

        Install = {
          WantedBy = [ "default.target" ];
        };
      };
    }

    # D-Bus service file for session bus activation
    (lib.mkIf cfg.enableDbus {
      home.file.".local/share/dbus-1/services/com.github.MartinLoeper.ClaudeOMeter.service".text = ''
        [D-BUS Service]
        Name=com.github.MartinLoeper.ClaudeOMeter
        Exec=${cfg.package}/bin/claude-o-meter daemon -i ${cfg.interval} -f ${cfg.stateFile}${lib.optionalString cfg.debug " --debug"} --dbus
        SystemdService=claude-o-meter.service
      '';
    })

    # Claude Code hooks integration
    (lib.mkIf cfg.enableClaudeCodeHooks {
      # Override interval default to 5 minutes when hooks are enabled
      # User can still override this explicitly
      services.claude-o-meter.interval = lib.mkDefault "5m";

      # Install the Claude Code plugin marketplace via symlink
      home.file.".claude/claude-o-meter-plugins".source = claudeCodeMarketplace;
    })
  ]);
}
