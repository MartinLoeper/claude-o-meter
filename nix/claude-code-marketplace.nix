# Marketplace package for Claude Code plugins managed by claude-o-meter
{ pkgs, claudeCodePlugin }:

pkgs.runCommand "claude-o-meter-marketplace" { } ''
  mkdir -p $out/.claude-plugin
  mkdir -p $out/claude-o-meter-refresh

  # Create marketplace.json with relative path
  cat > $out/.claude-plugin/marketplace.json << 'EOF'
  {
    "name": "claude-o-meter-plugins",
    "owner": {
      "name": "claude-o-meter",
      "email": "noreply@github.com"
    },
    "metadata": {
      "description": "Claude Code plugins managed by claude-o-meter",
      "version": "1.0.0"
    },
    "plugins": [{
      "name": "claude-o-meter-refresh",
      "source": "./claude-o-meter-refresh",
      "description": "Automatically refresh claude-o-meter metrics when Claude conversations end",
      "version": "1.0.0"
    }]
  }
  EOF

  # Copy the plugin into the marketplace
  cp -r ${claudeCodePlugin}/share/claude-o-meter-refresh/. $out/claude-o-meter-refresh/
''
