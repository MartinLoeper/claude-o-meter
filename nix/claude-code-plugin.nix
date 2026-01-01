# Claude Code plugin that triggers claude-o-meter refresh on conversation end
{ pkgs, claudeOMeterPackage }:

let
  pluginName = "claude-o-meter-refresh";
  version = "1.0.0";
in pkgs.stdenvNoCC.mkDerivation {
  pname = "claude-o-meter-cc-plugin";
  inherit version;

  # No source, we generate everything
  dontUnpack = true;

  installPhase = ''
    # Plugin content goes under share/ for easy copying into marketplace
    mkdir -p $out/share/${pluginName}

    # Create package.json with plugin metadata
    cat > $out/share/${pluginName}/package.json <<EOF
    {
      "name": "${pluginName}",
      "version": "${version}",
      "description": "Automatically refresh claude-o-meter metrics when Claude conversations end"
    }
    EOF

    # Create hooks.json with stop hook
    # Uses full path to claude-o-meter since Claude Code won't have it in PATH
    cat > $out/share/${pluginName}/hooks.json <<EOF
    {
      "hooks": {
        "Stop": [
          {
            "type": "command",
            "command": "${claudeOMeterPackage}/bin/claude-o-meter refresh",
            "timeout": 7
          }
        ]
      }
    }
    EOF
  '';

  meta = with pkgs.lib; {
    description = "Claude Code plugin for claude-o-meter refresh hooks";
    license = licenses.mit;
  };
}
