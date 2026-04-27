#!/usr/bin/env bash
# pwt.sh のユニットテスト
# 実行方法: bash tests/test_pwt.sh
# 依存: bash 4.0+ または zsh 5.0+

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PWT_SH="$SCRIPT_DIR/../pwt.sh"

# テストフレームワーク（最小実装）
_PASS=0
_FAIL=0

pass() { echo "  [PASS] $1"; _PASS=$((_PASS + 1)); }
fail() { echo "  [FAIL] $1"; _FAIL=$((_FAIL + 1)); }
assert_eq() {
    local desc="$1" got="$2" want="$3"
    if [ "$got" = "$want" ]; then pass "$desc"; else
        fail "$desc"
        echo "         got:  $(printf '%q' "$got")"
        echo "         want: $(printf '%q' "$want")"
    fi
}
assert_match() {
    local desc="$1" got="$2" pattern="$3"
    if [[ "$got" =~ $pattern ]]; then pass "$desc"; else
        fail "$desc"
        echo "         got:     $(printf '%q' "$got")"
        echo "         pattern: $pattern"
    fi
}
assert_true()  { local desc="$1"; shift; if "$@" >/dev/null 2>&1; then pass "$desc"; else fail "$desc"; fi; }
assert_false() { local desc="$1"; shift; if ! "$@" >/dev/null 2>&1; then pass "$desc"; else fail "$desc"; fi; }

# pwt.sh をソース（テスト用ダミー関数を先に定義して git 依存を回避）
git() { echo ""; return 0; }
ZSH_VERSION="${ZSH_VERSION:-}"
BASH_VERSION="${BASH_VERSION:-}"
# shellcheck source=../pwt.sh
source "$PWT_SH"

echo "=== _pwt_branch_slug ==="

assert_eq "feature/auth → feature-auth" \
    "$(_pwt_branch_slug 'feature/auth')" "feature-auth"
assert_eq "fix/foo/bar → fix-foo-bar" \
    "$(_pwt_branch_slug 'fix/foo/bar')" "fix-foo-bar"
assert_eq "main → main (変換なし)" \
    "$(_pwt_branch_slug 'main')" "main"

echo ""
echo "=== _pwt_validate_branch ==="

# モックを解除して本物の git check-ref-format を使用
unset -f git

assert_true  "有効: feature/foo"        _pwt_validate_branch "feature/foo"
assert_true  "有効: main"               _pwt_validate_branch "main"
assert_true  "有効: fix/123"            _pwt_validate_branch "fix/123"
assert_false "無効: 空文字"             _pwt_validate_branch ""
assert_false "無効: -d (ダッシュ始まり)" _pwt_validate_branch "-d"
assert_false "無効: --force"            _pwt_validate_branch "--force"
assert_false "無効: a..b (二重ドット)"  _pwt_validate_branch "a..b"

# モックを再設定
git() { echo ""; return 0; }

echo ""
echo "=== _pwt_realpath ==="

assert_eq "絶対パス → そのまま返す" \
    "$(_pwt_realpath '/absolute/path' '/any/dir')" \
    '/absolute/path'

_rp_tmp="$(mktemp -d)"
mkdir -p "$_rp_tmp/a/b"

assert_eq "相対パス → link_dir 基準で解決" \
    "$(_pwt_realpath 'somefile' "$_rp_tmp/a/b")" \
    "$_rp_tmp/a/b/somefile"

assert_eq "../ を含む相対パス → 正しく解決" \
    "$(_pwt_realpath '../sibling' "$_rp_tmp/a/b")" \
    "$_rp_tmp/a/sibling"

assert_eq "存在しないディレクトリ → 空文字列" \
    "$(_pwt_realpath 'file' '/nonexistent/__pwt_test__')" \
    ""

rm -rf "$_rp_tmp"

echo ""
echo "=== _pwt_parse_worktrees ==="

git() {
    cat <<'PORCELAIN'
worktree /repos/myapp
HEAD 0000000000000000000000000000000000000001
branch refs/heads/main

worktree /repos/myapp--feature-auth
HEAD 0000000000000000000000000000000000000002
branch refs/heads/feature/auth

worktree /repos/myapp--detached
HEAD 0000000000000000000000000000000000000003
detached

PORCELAIN
}

_parsed="$(_pwt_parse_worktrees "/repos/myapp")"
assert_eq "メイン worktree: path/branch" \
    "$(printf '%s' "$_parsed" | sed -n '1p')" \
    "/repos/myapp	main"
assert_eq "サブ worktree: feature/auth" \
    "$(printf '%s' "$_parsed" | sed -n '2p')" \
    "/repos/myapp--feature-auth	feature/auth"
assert_eq "detached HEAD の worktree" \
    "$(printf '%s' "$_parsed" | sed -n '3p')" \
    "/repos/myapp--detached	detached"

git() { echo ""; return 0; }

echo ""
echo "=== _pwt_clean_symlinks ==="

_cs_src="$(mktemp -d)"
_cs_dest="$(mktemp -d)"

echo "content" > "$_cs_src/linked_file"
mkdir "$_cs_src/linked_dir"
ln -s "$_cs_src/linked_file" "$_cs_dest/linked_file"
ln -s "$_cs_src/linked_dir"  "$_cs_dest/linked_dir"
ln -s "/tmp"                  "$_cs_dest/other_link"

_pwt_clean_symlinks "$_cs_src" "$_cs_dest" >/dev/null

assert_false "src を指すリンク（ファイル）が削除される"    test -L "$_cs_dest/linked_file"
assert_false "src を指すリンク（ディレクトリ）が削除される" test -L "$_cs_dest/linked_dir"
assert_true  "他のリンクは残る"                            test -L "$_cs_dest/other_link"

rm -rf "$_cs_src" "$_cs_dest"

echo ""
echo "=== _pwt_split_worktreelinks ==="

_sp_tmp="$(mktemp -d)"
cat > "$_sp_tmp/config" <<'EOF'
# comment
.env
.env.*

[copy]
vendor/
node_modules/

[link]
.docker/
EOF

_sp_link="$_sp_tmp/link"
_sp_copy="$_sp_tmp/copy"
_pwt_split_worktreelinks "$_sp_tmp/config" "$_sp_link" "$_sp_copy"

assert_match "link パターンに .env が含まれる" "$(cat "$_sp_link")" '\.env'
assert_match "link パターンに .docker/ が含まれる" "$(cat "$_sp_link")" '\.docker/'
assert_match "copy パターンに vendor/ が含まれる" "$(cat "$_sp_copy")" 'vendor/'
assert_match "copy パターンに node_modules/ が含まれる" "$(cat "$_sp_copy")" 'node_modules/'

# link 側に vendor が含まれないことを確認
_sp_link_no_vendor=true
while IFS= read -r line; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    [[ "$line" == *vendor* ]] && _sp_link_no_vendor=false
done < "$_sp_link"
assert_eq "link パターンに vendor が含まれない" "$_sp_link_no_vendor" "true"

# copy 側に .env が含まれないことを確認
_sp_copy_no_env=true
while IFS= read -r line; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    [[ "$line" == *\.env* ]] && _sp_copy_no_env=false
done < "$_sp_copy"
assert_eq "copy パターンに .env が含まれない" "$_sp_copy_no_env" "true"

rm -rf "$_sp_tmp"

echo ""
echo "=== _pwt_split_worktreelinks (末尾空白) ==="

_sp2_tmp="$(mktemp -d)"
# [copy] の後に末尾空白がある場合もセクションヘッダとして認識されるか
printf '.env\n[copy]  \nvendor/\n' > "$_sp2_tmp/config"
_sp2_link="$_sp2_tmp/link"
_sp2_copy="$_sp2_tmp/copy"
_pwt_split_worktreelinks "$_sp2_tmp/config" "$_sp2_link" "$_sp2_copy"
assert_match "末尾空白あり [copy]: vendor が copy に含まれる" "$(cat "$_sp2_copy")" 'vendor/'
# link 側に vendor が含まれないことを確認
_sp2_link_has_vendor=false
while IFS= read -r line; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    [[ "$line" == *vendor* ]] && _sp2_link_has_vendor=true
done < "$_sp2_link"
assert_eq "末尾空白あり [copy]: link 側に vendor が含まれない" "$_sp2_link_has_vendor" "false"
rm -rf "$_sp2_tmp"

echo ""
echo "=== _pwt_has_patterns ==="

_hp_tmp="$(mktemp -d)"

echo "" > "$_hp_tmp/empty"
assert_false "空ファイルにはパターンなし" _pwt_has_patterns "$_hp_tmp/empty"

printf '# comment only\n\n' > "$_hp_tmp/comments"
assert_false "コメントのみはパターンなし" _pwt_has_patterns "$_hp_tmp/comments"

printf '# comment\n.env\n' > "$_hp_tmp/with_pattern"
assert_true "パターンありで true" _pwt_has_patterns "$_hp_tmp/with_pattern"

printf '   \n  \n' > "$_hp_tmp/spaces"
assert_false "空白のみの行はパターンなし" _pwt_has_patterns "$_hp_tmp/spaces"

rm -rf "$_hp_tmp"

echo ""
echo "=== _pwt_create_symlinks ==="

_cr_src="$(mktemp -d)"
_cr_dest="$(mktemp -d)"

echo "secret" > "$_cr_src/.env"
mkdir "$_cr_src/node_modules"
mkdir "$_cr_src/vendor"
echo "autoload" > "$_cr_src/vendor/autoload.php"

cat > "$_cr_dest/.worktreelinks" <<'EOF'
.env
node_modules
[copy]
vendor
EOF

# git ls-files --exclude-from をモック
# exclude-from のパスから link/copy を判定して適切なエントリを返す
git() {
    if [[ "$*" == *"ls-files"* ]]; then
        local exclude_file=""
        local arg
        for arg in "$@"; do
            if [[ "$arg" == --exclude-from=* ]]; then
                exclude_file="${arg#--exclude-from=}"
                break
            fi
        done
        if [ -n "$exclude_file" ] && grep -q 'vendor' "$exclude_file" 2>/dev/null; then
            printf 'vendor\0'
        else
            printf '.env\0node_modules\0'
        fi
        return 0
    fi
    echo ""
}

_pwt_create_symlinks "$_cr_src" "$_cr_dest" >/dev/null

assert_true  ".env リンクが作成される"           test -L "$_cr_dest/.env"
assert_true  "node_modules リンクが作成される"    test -L "$_cr_dest/node_modules"
assert_eq    ".env のリンク先が正しい" \
    "$(readlink "$_cr_dest/.env")" "$_cr_src/.env"
assert_false "vendor はリンクではない（コピー）"  test -L "$_cr_dest/vendor"
assert_true  "vendor がディレクトリとして存在"    test -d "$_cr_dest/vendor"
assert_true  "vendor 内のファイルがコピーされている" test -f "$_cr_dest/vendor/autoload.php"

rm -rf "$_cr_src" "$_cr_dest"
git() { echo ""; return 0; }

echo ""
echo "=== _pwt_create_symlinks (symlink → copy 上書き) ==="

_co_src="$(mktemp -d)"
_co_dest="$(mktemp -d)"

mkdir "$_co_src/vendor"
echo "autoload" > "$_co_src/vendor/autoload.php"

# dest に既存のシンボリックリンク（別の場所を指す）を配置
_co_other="$(mktemp -d)"
ln -s "$_co_other" "$_co_dest/vendor"

cat > "$_co_dest/.worktreelinks" <<'EOF'
[copy]
vendor
EOF

git() {
    if [[ "$*" == *"ls-files"* ]]; then
        printf 'vendor\0'
        return 0
    fi
    echo ""
}

_pwt_create_symlinks "$_co_src" "$_co_dest" >/dev/null

assert_false "既存 symlink がコピーで上書きされる（リンクではない）" test -L "$_co_dest/vendor"
assert_true  "コピー後に実ディレクトリとして存在" test -d "$_co_dest/vendor"
assert_true  "コピー内のファイルが存在" test -f "$_co_dest/vendor/autoload.php"

rm -rf "$_co_src" "$_co_dest" "$_co_other"
git() { echo ""; return 0; }

echo ""
echo "=== _pwt_resolve_context (pwt.worktreeDir / worktreePrefix) ==="

# git モック: worktree list + config --get を模倣
_rc_tmp="$(mktemp -d)"
mkdir -p "$_rc_tmp/myapp"
_rc_mock_worktree_dir=""
_rc_mock_prefix=""

git() {
    if [[ "$*" == *"worktree list"* ]]; then
        printf 'worktree %s\nHEAD 0000\nbranch refs/heads/main\n\n' "$_rc_tmp/myapp"
        return 0
    fi
    if [[ "$*" == *"config --get pwt.worktreeDir"* ]]; then
        if [ -n "$_rc_mock_worktree_dir" ]; then
            echo "$_rc_mock_worktree_dir"
            return 0
        fi
        return 1  # 未設定
    fi
    if [[ "$*" == *"config --get pwt.worktreePrefix"* ]]; then
        if [ -n "$_rc_mock_prefix" ]; then
            echo "$_rc_mock_prefix"
            return 0
        fi
        return 1  # 未設定
    fi
    echo ""
}

# pwt.worktreeDir 未設定 → 親ディレクトリがそのまま work_base / use_prefix=true (auto)
unset GIT_PARALLEL_WORKTREES_BASE 2>/dev/null || true
_rc_result="$(_pwt_resolve_context)"
IFS=$'\t' read -r _rc_root _rc_name _rc_base _rc_prefix <<< "$_rc_result"
assert_eq "worktreeDir 未設定: work_base はリポジトリの親" "$_rc_base" "$_rc_tmp"
assert_eq "worktreeDir 未設定: use_prefix=true (auto)" "$_rc_prefix" "true"

# pwt.worktreeDir=.worktrees → 親配下のサブディレクトリ / use_prefix=true (auto)
_rc_mock_worktree_dir=".worktrees"
_rc_result="$(_pwt_resolve_context)"
IFS=$'\t' read -r _rc_root _rc_name _rc_base _rc_prefix <<< "$_rc_result"
assert_eq "worktreeDir=.worktrees: work_base にサブディレクトリが追加" "$_rc_base" "$_rc_tmp/.worktrees"
assert_eq "worktreeDir=.worktrees: use_prefix=true (auto)" "$_rc_prefix" "true"
assert_true "worktreeDir=.worktrees: ディレクトリが自動作成される" test -d "$_rc_tmp/.worktrees"

# pwt.worktreeDir=./.worktrees → main repo 内配置 / use_prefix=false (auto)
_rc_mock_worktree_dir="./.worktrees"
_rc_result="$(_pwt_resolve_context)"
IFS=$'\t' read -r _rc_root _rc_name _rc_base _rc_prefix <<< "$_rc_result"
assert_eq "worktreeDir=./.worktrees: work_base は main repo 内" "$_rc_base" "$_rc_tmp/myapp/.worktrees"
assert_eq "worktreeDir=./.worktrees: use_prefix=false (auto)" "$_rc_prefix" "false"
assert_true "worktreeDir=./.worktrees: ディレクトリが自動作成される" test -d "$_rc_tmp/myapp/.worktrees"

# worktreePrefix=repo で強制 prefix 付与
_rc_mock_prefix="repo"
_rc_result="$(_pwt_resolve_context)"
IFS=$'\t' read -r _rc_root _rc_name _rc_base _rc_prefix <<< "$_rc_result"
assert_eq "worktreePrefix=repo: use_prefix=true (内配置でも強制付与)" "$_rc_prefix" "true"

# worktreePrefix=none で強制 prefix 省略
_rc_mock_prefix="none"
_rc_mock_worktree_dir=".worktrees"
_rc_result="$(_pwt_resolve_context)"
IFS=$'\t' read -r _rc_root _rc_name _rc_base _rc_prefix <<< "$_rc_result"
assert_eq "worktreePrefix=none: use_prefix=false (外配置でも省略)" "$_rc_prefix" "false"

# worktreePrefix=auto は未設定と同じ扱い
_rc_mock_prefix="auto"
_rc_mock_worktree_dir="./.worktrees"
_rc_result="$(_pwt_resolve_context)"
IFS=$'\t' read -r _rc_root _rc_name _rc_base _rc_prefix <<< "$_rc_result"
assert_eq "worktreePrefix=auto + ./X: use_prefix=false" "$_rc_prefix" "false"

# worktreePrefix に不正な値を指定するとエラー
_rc_mock_prefix="invalid"
assert_false "worktreePrefix: 不正な値はエラー" _pwt_resolve_context

_rc_mock_prefix=""

# pwt.worktreeDir に絶対パスはエラー
_rc_mock_worktree_dir="/absolute/path"
assert_false "worktreeDir: 絶対パスはエラー" _pwt_resolve_context

# pwt.worktreeDir に .. を含むパスはエラー
_rc_mock_worktree_dir="../escape"
assert_false "worktreeDir: .. を含むパスはエラー" _pwt_resolve_context

# pwt.worktreeDir=./ 単独はエラー（main repo 自身を指すため）
_rc_mock_worktree_dir="./"
assert_false "worktreeDir=./: main repo 自身はエラー" _pwt_resolve_context

# pwt.worktreeDir=./../escape は ../ 扱いでエラー
_rc_mock_worktree_dir="./../escape"
assert_false "worktreeDir=./../: .. を含むためエラー" _pwt_resolve_context

_rc_mock_worktree_dir=""
rm -rf "$_rc_tmp"
git() { echo ""; return 0; }

echo ""
echo "=== _pwt_wt_path ==="

assert_eq "use_prefix=true: <base>/<repo>--<slug>" \
    "$(_pwt_wt_path '/parent/.worktrees' 'myapp' 'true' 'feature-auth')" \
    "/parent/.worktrees/myapp--feature-auth"

assert_eq "use_prefix=false: <base>/<slug> のみ" \
    "$(_pwt_wt_path '/main/.worktrees' 'myapp' 'false' 'feature-auth')" \
    "/main/.worktrees/feature-auth"

assert_eq "use_prefix=true: 末尾スラッシュの正規化" \
    "$(_pwt_wt_path '/parent/.worktrees/' 'myapp' 'true' 'slug')" \
    "/parent/.worktrees/myapp--slug"

echo ""
echo "=== _pwt_resolve_add_path ==="

assert_eq "バレネーム: work_base 配下に prefix 付きで配置" \
    "$(_pwt_resolve_add_path 'review_1' '/parent/.worktrees' 'myapp' 'true')" \
    "/parent/.worktrees/myapp--review_1"

assert_eq "バレネーム + use_prefix=false: prefix なし" \
    "$(_pwt_resolve_add_path 'review_1' '/main/.worktrees' 'myapp' 'false')" \
    "/main/.worktrees/review_1"

assert_eq "絶対パス: そのまま返す" \
    "$(_pwt_resolve_add_path '/tmp/wt' '/ignored' 'myapp' 'true')" \
    "/tmp/wt"

# 相対パス（/ を含む）は cwd 起点に展開される
_rap_cwd_expected="$(pwd)/sub/wt"
assert_eq "相対パス (/ 含む): cwd 起点で絶対パス化" \
    "$(_pwt_resolve_add_path 'sub/wt' '/ignored' 'myapp' 'true')" \
    "$_rap_cwd_expected"

assert_false "バレネームに .. を含むとエラー" \
    _pwt_resolve_add_path '..foo' '/base' 'myapp' 'true'

echo ""
echo "=============================="
echo "テスト結果: ${_PASS} passed, ${_FAIL} failed"
echo "=============================="

[ "$_FAIL" -eq 0 ] && exit 0 || exit 1
