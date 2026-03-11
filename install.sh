#!/usr/bin/env bash
set -euo pipefail

# pwt インストーラー
# pwt.sh を ~/.local/share/pwt/pwt.sh にシンボリックリンクとして配置する。
#
# Homebrew tap でインストールした場合は不要（formula が配置を担う）。
# 手動インストール時に使用する。

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PWT_SH="$SCRIPT_DIR/pwt.sh"
INSTALL_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/pwt"
INSTALL_TARGET="$INSTALL_DIR/pwt.sh"
SOURCE_LINE='source "${XDG_DATA_HOME:-$HOME/.local/share}/pwt/pwt.sh"'

usage() {
    cat <<'USAGE'
pwt インストーラー

使用方法:
  install.sh              シンボリックリンクを作成
  install.sh --uninstall  シンボリックリンクを削除
  install.sh --status     現在の状態を表示
  install.sh -h|--help    ヘルプを表示
USAGE
}

install() {
    echo "=== pwt インストール ==="

    mkdir -p "$INSTALL_DIR"

    if [ -L "$INSTALL_TARGET" ]; then
        echo "[更新] $INSTALL_TARGET"
        rm "$INSTALL_TARGET"
    elif [ -f "$INSTALL_TARGET" ]; then
        echo "[バックアップ] 既存の pwt.sh → pwt.sh.bak"
        mv "$INSTALL_TARGET" "${INSTALL_TARGET}.bak"
    else
        echo "[作成] $INSTALL_TARGET"
    fi
    ln -s "$PWT_SH" "$INSTALL_TARGET"

    echo ""
    echo "=== 完了 ==="
    echo ""

    local shell_rc=""
    if [ -n "${ZSH_VERSION:-}" ] || [ "$(basename "${SHELL:-}")" = "zsh" ]; then
        shell_rc="$HOME/.zshrc"
    else
        shell_rc="$HOME/.bashrc"
    fi

    if grep -q 'pwt/pwt.sh' "$shell_rc" 2>/dev/null; then
        echo "$shell_rc に source 済み"
    else
        echo "以下を $shell_rc に追加してください:"
        echo "  $SOURCE_LINE"
    fi
}

uninstall() {
    echo "=== pwt アンインストール ==="

    if [ -L "$INSTALL_TARGET" ]; then
        echo "[削除] $INSTALL_TARGET"
        rm "$INSTALL_TARGET"
        # ディレクトリが空になったら削除
        rmdir "$INSTALL_DIR" 2>/dev/null && echo "[削除] $INSTALL_DIR" || true
    else
        echo "インストールされていません"
    fi

    echo ""
    echo "=== 完了 ==="
}

status() {
    echo "=== pwt 状態 ==="
    if [ -L "$INSTALL_TARGET" ]; then
        echo "  インストール済み: $(readlink "$INSTALL_TARGET")"
    elif [ -f "$INSTALL_TARGET" ]; then
        echo "  実ファイル (リンクではない): $INSTALL_TARGET"
    else
        echo "  未インストール"
    fi
}

case "${1:-}" in
    --uninstall) uninstall ;;
    --status)    status ;;
    -h|--help)   usage ;;
    "")          install ;;
    *)           echo "不明なオプション: $1"; usage; exit 1 ;;
esac
