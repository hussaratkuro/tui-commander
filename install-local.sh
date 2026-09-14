#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
build_dir="$(mktemp -d)"
trap 'rm -rf -- "$build_dir"' EXIT
commander_go_cache="$build_dir/go-cache"

cd "$project_dir"
GOCACHE="$commander_go_cache" go build -trimpath -o "$build_dir/tui-commander" .
install -Dm755 "$build_dir/tui-commander" "$HOME/.local/bin/tui-commander"
install -Dm755 contrib/open-tui-commander "$HOME/.local/bin/open-tui-commander"
install -Dm644 contrib/tui-commander.desktop "$HOME/.local/share/applications/tui-commander.desktop"
install -Dm644 assets/tui-commander.png "$HOME/.local/share/icons/tui-commander/tui-commander.png"

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$HOME/.local/share/applications"
fi

echo "Installed tui-commander in $HOME/.local/bin"
