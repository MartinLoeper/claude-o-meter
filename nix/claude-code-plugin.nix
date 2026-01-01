# Claude Code plugin that triggers claude-o-meter refresh on conversation end
{ pkgs, claudeOMeterPackage }:

pkgs.stdenvNoCC.mkDerivation {
  pname = "claude-o-meter-cc-plugin";
  version = "1.0.0";

  # No source, we generate everything
  dontUnpack = true;

  installPhase = ''
    mkdir -p $out

    # Create package.json with plugin metadata
    cat > $out/package.json <<EOF
    {
      "name": "claude-o-meter-refresh",
      "version": "1.0.0",
      "description": "Automatically refresh claude-o-meter metrics when Claude conversations end"
    }
    EOF

    # Create hooks.json with stop hook
    # Uses full path to claude-o-meter since Claude Code won't have it in PATH
    cat > $out/hooks.json <<EOF
    {
      "hooks": {
        "Stop": [
          {
            "matcher": "",
            "commands": ["${claudeOMeterPackage}/bin/claude-o-meter refresh"]
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
