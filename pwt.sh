#!/usr/bin/env bash
# pwt - Git Parallel Worktrees
# ブランチ単位で worktree を動的に作成・管理（bash / zsh 両対応）
#
# このファイルは source して使用します:
#   source ~/.local/share/pwt/pwt.sh
#
# セットアップ:
#   echo 'source "${XDG_DATA_HOME:-$HOME/.local/share}/pwt/pwt.sh"' >> ~/.zshrc
#
# 使い方:
#   pwt                                       worktree 一覧（番号付き）
#   pwt switch <番号|名前>                    worktree に移動
#   pwt switch -c [-b <branch>] <path> [<commit-ish>]  worktree を作成して移動
#   pwt add [-b <branch>] [-B <branch>] [--detach]
#           <path> [<commit-ish>]             worktree を作成（移動しない）
#   pwt list                                  worktree 一覧（明示的）
#   pwt remove <branch|name|.>                worktree を削除（. は現在の worktree）
#   pwt init                                  .worktreelinks を生成
#   pwt sync [-f|--force]                     シンボリックリンクを再同期（-f で copy 対象の実体を上書き）
#   pwt unsync                                シンボリックリンクを全削除

# =============================================================================
# 内部ヘルパー関数
# =============================================================================

# メインリポジトリのルートパスを取得
# git worktree list --porcelain の1行目はメインリポジトリを指す（git の仕様）
_pwt_project_root() {
    local line
    while IFS= read -r line; do
        if [[ "$line" == "worktree "* ]]; then
            printf '%s' "${line#worktree }"
            return
        fi
    done < <(git worktree list --porcelain 2>/dev/null)
}

# pwt add の <path> 引数を絶対パスに解決
# - バレネーム (/ を含まない): work_base 配下に配置（pwt.worktreePrefix 設定を反映）
# - / を含む or 絶対パス: そのまま使う（git worktree add と同じ挙動）
# 引数: path_arg  work_base  project_name  use_prefix
# stdout: 解決後の絶対パス（失敗時は非ゼロ終了）
_pwt_resolve_add_path() {
    local path_arg="$1" work_base="$2" project_name="$3" use_prefix="$4"
    if [[ "$path_arg" == /* ]]; then
        printf '%s' "${path_arg%/}"
        return 0
    fi
    if [[ "$path_arg" == */* ]]; then
        printf '%s/%s' "$(pwd)" "${path_arg%/}"
        return 0
    fi
    if [[ "$path_arg" == *..* ]]; then
        echo "エラー: 不正な <path> (.. を含む): $path_arg" >&2
        return 1
    fi
    _pwt_wt_path "$work_base" "$project_name" "$use_prefix" "$path_arg"
}

# シンボリックリンクのターゲットを絶対パスに解決（macOS / Linux 両対応）
# $1: readlink の出力（相対または絶対パス）
# $2: シンボリックリンク自身が置かれているディレクトリ
_pwt_realpath() {
    local target="$1" link_dir="$2"
    if [[ "$target" == /* ]]; then
        echo "$target"
        return
    fi
    local dir base resolved_dir
    if [[ "$target" == */* ]]; then dir="${target%/*}"; else dir="."; fi
    base="${target##*/}"
    resolved_dir="$(cd "$link_dir/$dir" 2>/dev/null && pwd)"
    [ -n "$resolved_dir" ] && echo "$resolved_dir/$base" || echo ""
}

# git worktree list --porcelain をパース
# 出力: "path\tbranch" の行リスト（detached HEAD の場合は "detached"）
_pwt_parse_worktrees() {
    local root="$1"
    # 注意: zsh では path は PATH に連動する特殊変数のため wt_path を使用
    # git porcelain は各 worktree について "branch <ref>" または "detached" のいずれかを出力する。
    # detached の場合は branch が設定されないため、出力時に "${branch:-detached}" でフォールバックする。
    local wt_path="" branch=""
    while IFS= read -r line; do
        if [[ "$line" == "worktree "* ]]; then
            if [ -n "$wt_path" ]; then
                printf '%s\t%s\n' "$wt_path" "${branch:-detached}"
            fi
            wt_path="${line#worktree }"
            branch=""
        elif [[ "$line" == "branch "* ]]; then
            local raw_branch="${line#branch }"
            branch="${raw_branch#refs/heads/}"
        fi
    done < <(git -C "$root" worktree list --porcelain 2>/dev/null)
    if [ -n "$wt_path" ]; then
        printf '%s\t%s\n' "$wt_path" "${branch:-detached}"
    fi
}

# project_root / project_name / work_base / use_prefix をタブ区切りで stdout に出力
# eval を使わず stdout 返却方式とすることでコードインジェクションを防ぐ
# 使い方:
#   local _ctx
#   _ctx="$(_pwt_resolve_context)" || return 1
#   IFS=$'\t' read -r project_root project_name work_base use_prefix <<< "$_ctx"
#
# use_prefix は "true" / "false" で、worktree ディレクトリ名に <repo>-- を付けるか否か。
# pwt.worktreeDir が "./" で始まる場合は main repo 自身を base とし、prefix デフォルトは false。
# pwt.worktreePrefix (repo|none|auto) で明示的に上書き可能。
_pwt_resolve_context() {
    local root
    root="$(_pwt_project_root)"
    if [ -z "$root" ]; then
        echo "エラー: git リポジトリ内で実行してください" >&2
        return 1
    fi

    local base
    if [ -n "${GIT_PARALLEL_WORKTREES_BASE:-}" ]; then
        if [[ "$GIT_PARALLEL_WORKTREES_BASE" != /* ]]; then
            echo "エラー: GIT_PARALLEL_WORKTREES_BASE は絶対パスで指定してください" >&2
            return 1
        fi
        if [ ! -d "$GIT_PARALLEL_WORKTREES_BASE" ]; then
            echo "エラー: GIT_PARALLEL_WORKTREES_BASE が存在しません: $GIT_PARALLEL_WORKTREES_BASE" >&2
            return 1
        fi
        base="$GIT_PARALLEL_WORKTREES_BASE"
    else
        base="${root%/*}"
    fi

    # git config pwt.worktreeDir: worktree を専用サブディレクトリに格納する
    # 設定例:
    #   ".worktrees"    → {parent}/.worktrees/<repo>--<slug>    (親配下・主力)
    #   "./.worktrees"  → {main_repo}/.worktrees/<slug>         (main repo 内配置)
    local wt_dir use_main_base=false
    wt_dir="$(git -C "$root" config --get pwt.worktreeDir 2>/dev/null || true)"
    if [[ "$wt_dir" == ./* ]]; then
        use_main_base=true
        wt_dir="${wt_dir#./}"
        if [ -z "$wt_dir" ]; then
            echo "エラー: pwt.worktreeDir = './' は main repo 自身を指すため指定できません" >&2
            return 1
        fi
    fi
    if [ -n "$wt_dir" ]; then
        if [[ "$wt_dir" == /* ]] || [[ "$wt_dir" == *..* ]]; then
            echo "エラー: pwt.worktreeDir は相対パス（サブディレクトリ名）で指定してください" >&2
            return 1
        fi
        if [ "$use_main_base" = "true" ]; then
            base="$root"
        fi
        base="${base%/}/${wt_dir}"
        if [ ! -d "$base" ]; then
            mkdir -p "$base" || {
                echo "エラー: ディレクトリの作成に失敗しました: $base" >&2
                return 1
            }
        fi
    fi

    # pwt.worktreePrefix: worktree ディレクトリ名に <repo>-- を付けるかを明示指定
    #   "repo" → 常に付ける   /  "none" → 常に付けない  /  "auto" (既定) → 配置先から推論
    local prefix_config use_prefix
    prefix_config="$(git -C "$root" config --get pwt.worktreePrefix 2>/dev/null || true)"
    case "$prefix_config" in
        repo)       use_prefix=true ;;
        none)       use_prefix=false ;;
        ""|auto)
            # auto: main repo 内配置なら付けない、それ以外は付ける
            if [ "$use_main_base" = "true" ]; then
                use_prefix=false
            else
                use_prefix=true
            fi
            ;;
        *)
            echo "エラー: pwt.worktreePrefix は repo|none|auto のいずれかを指定してください: $prefix_config" >&2
            return 1
            ;;
    esac

    printf '%s\t%s\t%s\t%s' "$root" "${root##*/}" "$base" "$use_prefix"
}

# worktree ディレクトリ名を組み立てる
# 引数: work_base  project_name  use_prefix(true|false)  slug
_pwt_wt_path() {
    local work_base="$1" project_name="$2" use_prefix="$3" slug="$4"
    if [ "$use_prefix" = "true" ]; then
        printf '%s/%s--%s' "${work_base%/}" "$project_name" "$slug"
    else
        printf '%s/%s' "${work_base%/}" "$slug"
    fi
}

# ブランチ名のバリデーション（git check-ref-format + フラグインジェクション防止）
_pwt_validate_branch() {
    local branch="$1"
    if [ -z "$branch" ]; then
        echo "エラー: ブランチ名が空です" >&2
        return 1
    fi
    if [[ "$branch" == -* ]]; then
        echo "エラー: 不正なブランチ名（'-' で始まっている）: $branch" >&2
        return 1
    fi
    if ! git check-ref-format --branch "$branch" >/dev/null 2>&1; then
        echo "エラー: 不正なブランチ名: $branch" >&2
        return 1
    fi
}

# .worktreelinks を .gitignore から生成
_pwt_generate_worktreelinks() {
    local project_root="$1"
    local config="$project_root/.worktreelinks"

    if [ -f "$config" ]; then
        echo ".worktreelinks は既に存在します: $config"
        return
    fi

    cat > "$config" <<'EOF'
# .worktreelinks — worktree にシンボリックリンクするファイル/ディレクトリのパターン
#
# リンクしたいパターンのコメント (#) を外してください。
# パターンは .gitignore と同じルールで解釈されます:
#   .env            どの階層でもマッチ (/ を含まないパターン)
#   path/to/file    ルートからの相対パス (/ を含むパターン)
#   *.log           ワイルドカード
#
# [ライブラリ (node_modules, vendor 等) について]
# シンボリックリンクにすると新しい worktree で即作業開始できます。
# ライブラリ更新が必要な worktree では:
#   1. このファイルから該当パターンをコメントアウト
#   2. pwt sync  → シンボリックリンクが外れる
#   3. npm install 等で実体をインストール
#   他の worktree には影響しません。
#
# [git 管理について]
# このファイルをコミットするとチームで設定を共有できます。
# gitignore に追加した場合も、worktree 作成時に自動でコピーされます。
# いずれの場合も各 worktree が独立したコピーを持ちます。
#
# [コピーモード]
# Docker 等でシンボリックリンクが使えない場合、[copy] セクションに
# パターンを書くと実体をコピーします。
#   .env              ← シンボリックリンク（デフォルト）
#   [copy]
#   vendor/           ← コピー
# [link] で再びシンボリックリンクモードに戻せます。
#
# [制約]
# リンク/コピー対象は git ls-files --others --ignored で列挙されるファイル/ディレクトリに限ります。
# つまり、メインリポジトリの .gitignore（または .git/info/exclude）で無視されているものが対象です。

EOF

    local gitignore="$project_root/.gitignore"
    if [ -f "$gitignore" ]; then
        echo "# --- .gitignore の内容 ---" >> "$config"
        while IFS= read -r line; do
            if [[ -z "$line" ]]; then
                echo "" >> "$config"
            elif [[ "$line" == \#* ]]; then
                echo "$line" >> "$config"
            else
                echo "# $line" >> "$config"
            fi
        done < "$gitignore"
    else
        echo "# .gitignore が見つかりませんでした" >> "$config"
        echo "# パターンを手動で追加してください" >> "$config"
    fi
}

# worktree 内のメインリポジトリへのシンボリックリンクをすべて削除
_pwt_clean_symlinks() {
    local src_root="$1" dest_root="$2"
    src_root="${src_root%/}"
    local count=0
    local raw_target resolved

    while IFS= read -r -d '' link; do
        raw_target=$(readlink "$link" 2>/dev/null) || continue

        resolved=$(_pwt_realpath "$raw_target" "${link%/*}")
        [ -z "$resolved" ] && resolved="$raw_target"

        if [[ "$resolved" == "$src_root/"* ]] || [[ "$resolved" == "$src_root" ]]; then
            rm -f "$link"
            count=$((count + 1))
        fi
    # node_modules などのネスト構造を考慮して maxdepth 20 とする（一般的な JS プロジェクトで十分）
    done < <(find "$dest_root" -maxdepth 20 -type l -not -path "*/.git/*" -print0 2>/dev/null)

    if [ "$count" -gt 0 ]; then echo "  ${count} 個のシンボリックリンクを削除"; fi
}

# .worktreelinks を [link] / [copy] セクションごとに分離
# $1: .worktreelinks のパス
# $2: link パターン出力先（一時ファイル）
# $3: copy パターン出力先（一時ファイル）
_pwt_split_worktreelinks() {
    local config="$1" link_file="$2" copy_file="$3"
    local mode="link"
    local section_copy_re='^\[copy\][[:space:]]*$'
    local section_link_re='^\[link\][[:space:]]*$'

    while IFS= read -r line; do
        # セクションヘッダ
        if [[ "$line" =~ $section_copy_re ]]; then
            mode="copy"
            continue
        elif [[ "$line" =~ $section_link_re ]]; then
            mode="link"
            continue
        fi

        if [ "$mode" = "copy" ]; then
            echo "$line" >> "$copy_file"
        else
            echo "$line" >> "$link_file"
        fi
    done < "$config"
}

# パターンファイルに有効なパターンが含まれるかチェック
_pwt_has_patterns() {
    local file="$1"
    while IFS= read -r line; do
        local trimmed="${line#"${line%%[![:space:]]*}"}"
        [[ -z "$trimmed" || "$line" == \#* ]] && continue
        return 0
    done < "$file"
    return 1
}

# git ls-files の結果を走査し、モード（link / copy）に応じてシンボリックリンクまたはコピーを作成
# $1: モード ("link" or "copy")
# $2: src_root  $3: dest_root  $4: パターンファイル  $5: count 書き込み先ファイル
# $6: force ("1" のとき copy モードで実ファイル/ディレクトリを削除して上書き)
_pwt_process_entries() {
    local mode="$1" src_root="$2" dest_root="$3" pattern_file="$4" count_file="$5"
    local force="${6:-0}"
    local count=0

    while IFS= read -r -d '' raw_entry; do
        local entry="${raw_entry%/}"
        [ -z "$entry" ] && continue

        # パストラバーサル防止（多層防御: git ls-files 出力は安全だが念のため）
        if [[ "$entry" =~ (^|/)\.\.(/|$) ]]; then
            echo "  [!] 不正なパスをスキップ: $entry" >&2
            continue
        fi

        local src="$src_root/$entry"
        local dest="$dest_root/$entry"
        [ -e "$src" ] || continue

        if [ "$mode" = "link" ]; then
            # 実体ファイル/ディレクトリが存在する場合はスキップ
            if [ -e "$dest" ] && [ ! -L "$dest" ]; then
                if [ -d "$dest" ]; then
                    echo "  [スキップ] $entry/ (実ディレクトリが存在します)"
                else
                    echo "  [スキップ] $entry (実ファイルが存在します)"
                fi
                continue
            fi
            mkdir -p "${dest%/*}"
            rm -f "$dest"
            ln -s "$src" "$dest"
            count=$((count + 1))
            if [ -d "$src" ]; then echo "  [リンク] $entry/"; else echo "  [リンク] $entry"; fi
        else
            # コピーモード: シンボリックリンクが残っていれば削除して上書き
            local overwritten=0
            if [ -L "$dest" ]; then
                rm -f "$dest"
            elif [ -e "$dest" ]; then
                if [ "$force" = "1" ]; then
                    rm -rf "$dest"
                    overwritten=1
                else
                    if [ -d "$dest" ]; then
                        echo "  [スキップ] $entry/ (実ディレクトリが存在します。-f で上書き)"
                    else
                        echo "  [スキップ] $entry (実ファイルが存在します。-f で上書き)"
                    fi
                    continue
                fi
            fi
            mkdir -p "${dest%/*}"
            local label="コピー"
            [ "$overwritten" = "1" ] && label="上書き"
            if [ -d "$src" ]; then
                cp -RP "$src" "$dest"
                echo "  [$label] $entry/"
            else
                cp "$src" "$dest"
                echo "  [$label] $entry"
            fi
            count=$((count + 1))
        fi
    done < <(git -C "$src_root" ls-files -z \
        --others --ignored --exclude-from="$pattern_file" --directory 2>/dev/null)

    echo "$count" > "$count_file"
}

# .worktreelinks のパターンに従いシンボリックリンク/コピーを作成
# git ls-files --exclude-from で git 自身にパターンマッチを委譲する
# $3: force ("1" のとき [copy] エントリで既存の実ファイル/ディレクトリを上書き)
_pwt_create_symlinks() {
    local src_root="$1" dest_root="$2"
    local force="${3:-0}"
    local config="$dest_root/.worktreelinks"

    if [ ! -f "$config" ]; then
        echo "  (.worktreelinks が見つかりません)"
        return
    fi

    # 既存のシンボリックリンクを削除
    _pwt_clean_symlinks "$src_root" "$dest_root"

    # .worktreelinks をパースして [link] / [copy] パターンを分離
    local link_file copy_file count_file
    link_file="$(mktemp)" || return 1
    copy_file="$(mktemp)" || { rm -f "$link_file"; return 1; }
    count_file="$(mktemp)" || { rm -f "$link_file" "$copy_file"; return 1; }
    _pwt_split_worktreelinks "$config" "$link_file" "$copy_file"

    local link_count=0 copy_count=0

    if _pwt_has_patterns "$link_file"; then
        _pwt_process_entries "link" "$src_root" "$dest_root" "$link_file" "$count_file" "0"
        link_count=$(cat "$count_file")
    fi

    if _pwt_has_patterns "$copy_file"; then
        _pwt_process_entries "copy" "$src_root" "$dest_root" "$copy_file" "$count_file" "$force"
        copy_count=$(cat "$count_file")
    fi

    rm -f "$link_file" "$copy_file" "$count_file"

    local total=$((link_count + copy_count))
    if [ "$total" -eq 0 ]; then
        echo "  (リンク/コピー対象がありません)"
    else
        [ "$link_count" -gt 0 ] && echo "  ${link_count} 個のシンボリックリンクを作成"
        [ "$copy_count" -gt 0 ] && echo "  ${copy_count} 個のファイル/ディレクトリをコピー"
    fi
}

# =============================================================================
# pwt - メインコマンド
# =============================================================================

pwt() {
    local first="${1:-}"

    if [ -z "$first" ]; then
        _pwt_cmd_list
        return $?
    fi

    case "$first" in
        switch)  shift; _pwt_cmd_switch  "$@"; return $? ;;
        add)     shift; _pwt_cmd_add     "$@"; return $? ;;
        list)    shift; _pwt_cmd_list    "$@"; return $? ;;
        remove)  shift; _pwt_cmd_remove  "$@"; return $? ;;
        sync)    shift; _pwt_cmd_sync    "$@"; return $? ;;
        unsync)  shift; _pwt_cmd_unsync  "$@"; return $? ;;
        init)    shift; _pwt_cmd_init    "$@"; return $? ;;
        help)    shift; _pwt_cmd_help    "$@"; return $? ;;
    esac

    echo "エラー: 不明なサブコマンド: $first" >&2
    echo "  pwt help でコマンド一覧を確認してください" >&2
    return 1
}

# ----------------------------------------------------------------
# list
_pwt_cmd_list() {
    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base use_prefix
    IFS=$'\t' read -r project_root project_name work_base use_prefix <<< "$_ctx"

    local current_root
    current_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"

    echo "=== $project_name ==="
    local i=0 wt_path branch
    while IFS=$'\t' read -r wt_path branch; do
        [ -z "$wt_path" ] && continue
        local marker=" "
        [ "$current_root" = "$wt_path" ] && marker=">"
        printf " %s%2d  %-45s  (%s)\n" "$marker" "$i" "$wt_path" "$branch"
        i=$((i + 1))
    done < <(_pwt_parse_worktrees "$project_root")
}

# ----------------------------------------------------------------
# switch
# 構文:
#   pwt switch <番号|名前>
#   pwt switch -c [-b <branch>] [-B <branch>] [--detach] <path> [<commit-ish>]
#     → -c に続く引数はそのまま pwt add に転送し、作成後に <path> へ移動する
_pwt_cmd_switch() {
    if [ "$#" -eq 0 ]; then
        echo "使い方: pwt switch <番号|名前>" >&2
        echo "        pwt switch -c [-b <branch>] [-B <branch>] [--detach] <path> [<commit-ish>]" >&2
        return 1
    fi

    if [ "$1" = "-c" ]; then
        shift
        if [ "$#" -eq 0 ]; then
            echo "エラー: -c の後ろに引数が必要です" >&2
            echo "使い方: pwt switch -c [-b <branch>] [-B <branch>] [--detach] <path> [<commit-ish>]" >&2
            return 1
        fi

        # add に転送する前に <path>（最初の positional）を抽出しておく
        # 値を取るフラグ (-b / -B / --reason) は次の arg を読み飛ばす
        local nav_target=""
        local end_of_options=false
        local skip_next=false
        local arg
        for arg in "$@"; do
            if [ "$skip_next" = "true" ]; then
                skip_next=false
                continue
            fi
            if [ "$end_of_options" = "true" ]; then
                nav_target="$arg"
                break
            fi
            case "$arg" in
                --)
                    end_of_options=true
                    ;;
                -b|-B|--reason)
                    skip_next=true
                    ;;
                -*)
                    ;;
                *)
                    nav_target="$arg"
                    break
                    ;;
            esac
        done

        _pwt_cmd_add "$@" || return 1

        if [ -z "$nav_target" ]; then
            echo "エラー: <path> を解決できません" >&2
            return 1
        fi
        # 絶対/相対パスでも _pwt_navigate がディレクトリ名一致を見るため basename を渡す
        if [[ "$nav_target" == */* ]]; then
            nav_target="${nav_target##*/}"
        fi
        _pwt_navigate "$nav_target"
        return $?
    fi

    if [ "$#" -gt 1 ]; then
        echo "エラー: 余分な引数: $2" >&2
        return 1
    fi
    _pwt_navigate "$1"
}

# worktree に cd する内部ヘルパー
_pwt_navigate() {
    local target="$1"

    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base use_prefix
    IFS=$'\t' read -r project_root project_name work_base use_prefix <<< "$_ctx"

    local wt_data
    wt_data=$(_pwt_parse_worktrees "$project_root")

    local wt_path branch
    if [[ "$target" =~ ^[0-9]+$ ]]; then
        local idx=0
        while IFS=$'\t' read -r wt_path branch; do
            [ -z "$wt_path" ] && continue
            if [ "$idx" -eq "$target" ]; then
                cd "$wt_path" || { echo "エラー: cd に失敗しました: $wt_path" >&2; return 1; }
                return 0
            fi
            idx=$((idx + 1))
        done <<< "$wt_data"
        echo "エラー: worktree $target は存在しません" >&2
        return 1
    fi

    local exact_path="" match_path="" match_count=0 partial_list=""
    while IFS=$'\t' read -r wt_path branch; do
        [ -z "$wt_path" ] && continue
        local dir_name="${wt_path##*/}"
        if [ "$branch" = "$target" ] || [ "$dir_name" = "$target" ]; then
            exact_path="$wt_path"
            break
        elif [[ "$branch" == *"$target"* ]] || [[ "$dir_name" == *"$target"* ]]; then
            match_path="$wt_path"
            match_count=$((match_count + 1))
            partial_list="${partial_list}  $branch  ($wt_path)"$'\n'
        fi
    done <<< "$wt_data"

    if [ -n "$exact_path" ]; then
        cd "$exact_path" || { echo "エラー: cd に失敗しました: $exact_path" >&2; return 1; }
        return 0
    fi

    if [ "$match_count" -eq 1 ]; then
        cd "$match_path" || { echo "エラー: cd に失敗しました: $match_path" >&2; return 1; }
        return 0
    elif [ "$match_count" -gt 1 ]; then
        echo "エラー: '$target' に複数の worktree がマッチします。より具体的な名前を指定してください:" >&2
        printf '%s' "$partial_list" >&2
        return 1
    fi

    echo "エラー: '$target' に一致する worktree が見つかりません" >&2
    echo "  pwt list で一覧を確認してください" >&2
    return 1
}

# ----------------------------------------------------------------
# add — git worktree add の薄いラッパー
# 構文: pwt add [-b <new-branch>] [-B <new-branch>] [--detach] [<git worktree add の他フラグ>] <path> [<commit-ish>]
#
# pwt 独自の挙動:
#   1. <path> がバレネーム（/ を含まない）の場合は work_base 配下に配置する。
#      それ以外（/ 含み・絶対パス）はそのまま git worktree add に渡す。
#   2. <path> が絶対パス / 相対パス明示 (./ ../) のいずれでもなく、
#      -b/-B/--detach および <commit-ish> がいずれも未指定の場合は
#      <path> をブランチ名とみなして自動推論する（auto branch mode）:
#        A) refs/heads/<path> が存在 → そのブランチをチェックアウト
#        B) refs/remotes/origin/<path> のみ存在 → -b で local を作成し origin 追従
#        C) どこにも無い → -b で HEAD ベースの新規ブランチを作成
#      ディレクトリ名は <path> をそのまま使い (work_base/<path>)、ブランチ名と
#      ディレクトリ構成を一致させる。bare name と / 含みの両方に適用される。
#      ./foo や ../foo のような相対パス明示はファイルシステムパスとして
#      そのまま git worktree add に渡される。
_pwt_cmd_add() {
    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base use_prefix
    IFS=$'\t' read -r project_root project_name work_base use_prefix <<< "$_ctx"

    local new_branch="" reset_branch=""
    local -a passthrough_flags=()
    local -a positional=()
    local end_of_options=false

    while [ "$#" -gt 0 ]; do
        if [ "$end_of_options" = "true" ]; then
            positional+=("$1")
            shift
            continue
        fi
        case "$1" in
            -b)
                shift
                if [ -z "${1:-}" ]; then
                    echo "エラー: -b にはブランチ名が必要です" >&2
                    return 1
                fi
                new_branch="$1"
                ;;
            -B)
                shift
                if [ -z "${1:-}" ]; then
                    echo "エラー: -B にはブランチ名が必要です" >&2
                    return 1
                fi
                reset_branch="$1"
                ;;
            -d|--detach|--no-detach \
            |-f|--force|--no-force \
            |--checkout|--no-checkout \
            |--lock|--no-lock \
            |--orphan|--no-orphan \
            |--track|--no-track \
            |--guess-remote|--no-guess-remote \
            |--relative-paths|--no-relative-paths \
            |-q|--quiet)
                passthrough_flags+=("$1")
                ;;
            --reason)
                shift
                if [ -z "${1:-}" ]; then
                    echo "エラー: --reason には値が必要です" >&2
                    return 1
                fi
                passthrough_flags+=("--reason" "$1")
                ;;
            --)
                end_of_options=true
                ;;
            -*)
                echo "エラー: 不明なオプション: $1" >&2
                echo "使い方: pwt add [-b <new-branch>] [-B <new-branch>] [--detach] <path> [<commit-ish>]" >&2
                return 1
                ;;
            *)
                positional+=("$1")
                ;;
        esac
        shift
    done

    # zsh は配列が 1-indexed のため $1/$2 経由でアクセスする（set -- で位置引数化）
    local pos_count="${#positional[@]}"
    if [ "$pos_count" -eq 0 ]; then
        echo "使い方: pwt add [-b <new-branch>] [-B <new-branch>] [--detach] <path> [<commit-ish>]" >&2
        return 1
    fi
    set -- "${positional[@]}"
    if [ "$pos_count" -gt 2 ]; then
        echo "エラー: 余分な引数: $3" >&2
        return 1
    fi

    local path_arg="$1"
    local commit_ish="${2:-}"

    if [ -z "$path_arg" ]; then
        echo "エラー: <path> が空です" >&2
        return 1
    fi
    if [[ "$path_arg" == -* ]]; then
        echo "エラー: 不正な <path> ('-' で始まっている): $path_arg" >&2
        return 1
    fi
    if [ -n "$commit_ish" ] && [[ "$commit_ish" == -* ]]; then
        echo "エラー: <commit-ish> が '-' で始まっています: $commit_ish" >&2
        return 1
    fi

    # auto branch mode: <path> をブランチ名とみなして work_base/<path> に配置する
    # 条件: <path> が絶対パス / 相対パス明示 (./ ../) でなく
    #       commit-ish/-b/-B/--detach がいずれも未指定
    # bare name (foo) と / 含み (feature/foo) の両方で DWIM (既存 → checkout / 無ければ新規)
    local auto_branch=false
    local detach_specified=false
    if [ "${#passthrough_flags[@]}" -gt 0 ]; then
        local _f
        for _f in "${passthrough_flags[@]}"; do
            case "$_f" in
                -d|--detach) detach_specified=true; break ;;
            esac
        done
    fi

    # 相対パス明示 (./ ../ . ..) は意図が「ファイルシステムパス」と明らかなため除外
    if [[ "$path_arg" != /* ]] \
        && [[ "$path_arg" != ./* ]] \
        && [[ "$path_arg" != ../* ]] \
        && [ "$path_arg" != "." ] \
        && [ "$path_arg" != ".." ] \
        && [ -z "$commit_ish" ] \
        && [ -z "$new_branch" ] \
        && [ -z "$reset_branch" ] \
        && [ "$detach_specified" = "false" ]
    then
        if git -C "$project_root" show-ref --verify --quiet "refs/heads/$path_arg"; then
            # A) ローカルブランチが存在 → そのまま checkout
            commit_ish="$path_arg"
            auto_branch=true
        elif git -C "$project_root" show-ref --verify --quiet "refs/remotes/origin/$path_arg"; then
            # B) origin にのみ存在 → 同名 local ブランチを origin 追従で作成
            new_branch="$path_arg"
            commit_ish="origin/$path_arg"
            auto_branch=true
        else
            # C) どこにも無い → HEAD ベースで新規ブランチを作成
            new_branch="$path_arg"
            auto_branch=true
        fi
    fi

    if [ -n "$new_branch" ] && [ -n "$reset_branch" ]; then
        echo "エラー: -b と -B は同時に指定できません" >&2
        return 1
    fi

    if [ -n "$new_branch" ]; then
        _pwt_validate_branch "$new_branch" || return 1
    fi
    if [ -n "$reset_branch" ]; then
        _pwt_validate_branch "$reset_branch" || return 1
    fi

    local wt_path
    if [ "$auto_branch" = "true" ]; then
        # auto branch mode: / を保持して work_base 配下に配置（ブランチ名と一致）
        wt_path="$(_pwt_wt_path "$work_base" "$project_name" "$use_prefix" "$path_arg")"
    else
        wt_path="$(_pwt_resolve_add_path "$path_arg" "$work_base" "$project_name" "$use_prefix")" || return 1
    fi
    wt_path="${wt_path%/}"

    if [ -e "$wt_path" ]; then
        echo "エラー: '$wt_path' は既に存在します" >&2
        return 1
    fi

    echo "=== Worktree 作成: $wt_path ==="

    local -a git_args=(worktree add)
    if [ "${#passthrough_flags[@]}" -gt 0 ]; then
        git_args+=("${passthrough_flags[@]}")
    fi
    if [ -n "$new_branch" ]; then
        git_args+=(-b "$new_branch")
    fi
    if [ -n "$reset_branch" ]; then
        git_args+=(-B "$reset_branch")
    fi
    git_args+=(-- "$wt_path")
    if [ -n "$commit_ish" ]; then
        git_args+=("$commit_ish")
    fi

    if ! git -C "$project_root" "${git_args[@]}"; then
        echo "エラー: worktree の作成に失敗しました" >&2
        return 1
    fi
    echo "  [+] worktree 作成: $wt_path"

    if [ ! -f "$wt_path/.worktreelinks" ]; then
        local src_links=""
        local current_root
        current_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
        if [ -n "$current_root" ] && [ -f "$current_root/.worktreelinks" ]; then
            src_links="$current_root/.worktreelinks"
        elif [ -f "$project_root/.worktreelinks" ]; then
            src_links="$project_root/.worktreelinks"
        fi
        if [ -n "$src_links" ]; then
            cp "$src_links" "$wt_path/.worktreelinks"
            echo "  [+] .worktreelinks をコピー"
        else
            echo "  [!] .worktreelinks が見つかりません (pwt init で生成できます)"
        fi
    fi

    if [ -f "$wt_path/.worktreelinks" ]; then
        echo ""
        _pwt_create_symlinks "$project_root" "$wt_path"
    fi

    echo ""
    echo "  作成完了: $wt_path"
    echo "  移動: pwt switch ${wt_path##*/}"
}

# ----------------------------------------------------------------
# remove
# 構文: pwt remove <branch|name|path|.>
#   - .         : 現在の worktree
#   - 完全一致  : ブランチ名 / ディレクトリ名 / 絶対パスのいずれかと完全一致
#   - 部分一致  : ブランチ名 / ディレクトリ名の部分文字列マッチ（一意なときのみ）
#
# pwt add がカスタム <path> を受けるようになり、ブランチ名 → slug → パスの
# 逆引きでは到達できないケースが出るため、git worktree list を走査する方式に
# 変更した。
_pwt_cmd_remove() {
    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base use_prefix
    IFS=$'\t' read -r project_root project_name work_base use_prefix <<< "$_ctx"

    local arg="${1:-}"
    if [ -z "$arg" ]; then
        echo "使い方: pwt remove <branch|name|.>" >&2
        return 1
    fi

    local wt_path=""
    if [ "$arg" = "." ]; then
        # '.' は現在の worktree を対象にする省略記法
        local current_root
        current_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
        if [ -z "$current_root" ]; then
            echo "エラー: git リポジトリ内で実行してください" >&2
            return 1
        fi
        if [ "$current_root" = "$project_root" ]; then
            echo "エラー: main リポジトリは削除できません（pwt remove . は worktree 内で使用）" >&2
            return 1
        fi
        wt_path="$current_root"
    else
        # git worktree list を走査してブランチ名 / ディレクトリ名 / パスでマッチ
        local exact_path="" match_path="" match_count=0 partial_list=""
        local p b dir_name
        while IFS=$'\t' read -r p b; do
            [ -z "$p" ] && continue
            [ "$p" = "$project_root" ] && continue  # main リポジトリは対象外
            dir_name="${p##*/}"
            if [ "$b" = "$arg" ] || [ "$dir_name" = "$arg" ] || [ "$p" = "$arg" ]; then
                exact_path="$p"
                break
            elif [[ "$b" == *"$arg"* ]] || [[ "$dir_name" == *"$arg"* ]]; then
                match_path="$p"
                match_count=$((match_count + 1))
                partial_list="${partial_list}  $b  ($p)"$'\n'
            fi
        done < <(_pwt_parse_worktrees "$project_root")

        if [ -n "$exact_path" ]; then
            wt_path="$exact_path"
        elif [ "$match_count" -eq 1 ]; then
            wt_path="$match_path"
        elif [ "$match_count" -gt 1 ]; then
            echo "エラー: '$arg' に複数の worktree がマッチします。より具体的な名前を指定してください:" >&2
            printf '%s' "$partial_list" >&2
            return 1
        else
            echo "エラー: '$arg' に一致する worktree が見つかりません" >&2
            echo "  pwt list で一覧を確認してください" >&2
            return 1
        fi
    fi

    if [ ! -d "$wt_path" ]; then
        echo "エラー: worktree のディレクトリが存在しません: $wt_path" >&2
        return 1
    fi

    local current_worktree_root
    current_worktree_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
    if [ "$current_worktree_root" = "$wt_path" ]; then
        echo "  カレントディレクトリが削除対象のため main リポジトリに移動します"
        cd "$project_root" || { echo "エラー: cd に失敗しました: $project_root" >&2; return 1; }
    fi

    local status_output
    status_output=$(git -C "$wt_path" status --short 2>/dev/null)
    if [ -n "$status_output" ]; then
        local status_count
        status_count=$(printf '%s\n' "$status_output" | grep -c '.')
        echo "  [!] 変更があります (${status_count} 件):"
        local _n=0
        while IFS= read -r _l && [ "$_n" -lt 20 ]; do
            printf '      %s\n' "$_l"; _n=$((_n + 1))
        done <<< "$status_output"
        [ "$status_count" -gt 20 ] && echo "      ... (${status_count} 件中 20 件を表示)"
        printf "  強制削除しますか？ [y/N] "
        local answer=""
        read -r -t 30 answer </dev/tty 2>/dev/null || true
        [[ "$answer" =~ ^[Yy]$ ]] || { echo "キャンセルしました"; return 1; }

        if ! git -C "$project_root" worktree remove --force -- "$wt_path"; then
            echo "エラー: worktree の削除に失敗しました: $wt_path" >&2
            echo "  手動で削除してください: rm -rf \"$wt_path\"" >&2
            return 1
        fi
    else
        if ! git -C "$project_root" worktree remove -- "$wt_path"; then
            echo "エラー: worktree の削除に失敗しました: $wt_path" >&2
            echo "  手動で削除してください: rm -rf \"$wt_path\"" >&2
            return 1
        fi
    fi

    echo "削除しました: $wt_path"
}

# ----------------------------------------------------------------
# sync
_pwt_cmd_sync() {
    local force=0
    while [ $# -gt 0 ]; do
        case "$1" in
            -f|--force) force=1; shift ;;
            --) shift; break ;;
            -*) echo "エラー: 不明なオプション: $1" >&2; return 1 ;;
            *)  echo "エラー: 余分な引数: $1" >&2; return 1 ;;
        esac
    done

    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base use_prefix
    IFS=$'\t' read -r project_root project_name work_base use_prefix <<< "$_ctx"

    local current_root
    current_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"

    if [ -z "$current_root" ] || [ "$current_root" = "$project_root" ]; then
        echo "エラー: worktree 内で実行してください (main リポジトリでは使用できません)" >&2
        return 1
    fi

    if [ ! -f "$current_root/.worktreelinks" ]; then
        if [ -f "$project_root/.worktreelinks" ]; then
            echo "  [!] .worktreelinks が見つかりません。main リポジトリからコピーします"
            cp "$project_root/.worktreelinks" "$current_root/.worktreelinks"
        else
            echo "エラー: .worktreelinks が見つかりません (pwt init で生成してください)" >&2
            return 1
        fi
    fi

    echo "=== シンボリックリンク同期: ${current_root##*/} ==="
    [ "$force" = "1" ] && echo "  (force: [copy] エントリの実ファイル/ディレクトリを上書きします)"
    _pwt_create_symlinks "$project_root" "$current_root" "$force"
}

# ----------------------------------------------------------------
# unsync
_pwt_cmd_unsync() {
    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base use_prefix
    IFS=$'\t' read -r project_root project_name work_base use_prefix <<< "$_ctx"

    local current_root
    current_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"

    if [ -z "$current_root" ] || [ "$current_root" = "$project_root" ]; then
        echo "エラー: worktree 内で実行してください (main リポジトリでは使用できません)" >&2
        return 1
    fi

    echo "=== シンボリックリンク全削除: ${current_root##*/} ==="
    _pwt_clean_symlinks "$project_root" "$current_root"
    echo ""
    echo "  再同期:              pwt sync"
    echo "  特定エントリのみ外す: .worktreelinks を編集 → pwt sync"
}

# ----------------------------------------------------------------
# init
_pwt_cmd_init() {
    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base use_prefix
    IFS=$'\t' read -r project_root project_name work_base use_prefix <<< "$_ctx"

    echo "=== .worktreelinks 生成: $project_name ==="
    _pwt_generate_worktreelinks "$project_root"
    echo ""
    echo "  次のステップ:"
    echo "    1. vim $project_root/.worktreelinks  (リンクしたいパターンのコメントを外す)"
    echo "    2. pwt add <branch>                  (worktree を作成)"
}

# ----------------------------------------------------------------
# help
_pwt_cmd_help() {
    echo 'pwt - Git Parallel Worktrees'
    echo ''
    echo '使い方（プロジェクトディレクトリ内で実行）:'
    echo '  pwt                                           worktree 一覧（番号付き・現在位置マーク）'
    echo ''
    echo '  pwt switch <番号|名前>                        worktree に移動'
    echo '  pwt switch -c [-b <branch>] [-B <branch>] [--detach]'
    echo '              <path> [<commit-ish>]             worktree を作成して移動'
    echo '  pwt add [-b <branch>] [-B <branch>] [--detach]'
    echo '          <path> [<commit-ish>]                 worktree を作成（移動しない）'
    echo ''
    echo '  <path> 解釈: バレネーム → work_base 配下に配置 / / 含み・絶対パス → そのまま git worktree add'
    echo '  auto branch mode: <path> が絶対パス・相対パス明示 (./ ../) でなく、-b/-B/--detach および'
    echo '                    <commit-ish> がいずれも未指定なら、<path> をブランチ名とみなして自動推論する'
    echo '                    (既存ローカル → checkout / origin のみ → 追従作成 / 無し → HEAD ベースで新規)'
    echo '  pwt list                                      worktree 一覧（明示的）'
    echo '  pwt remove <branch|name|.>                                       worktree を削除（. は現在の worktree / 対象なら main へ移動）'
    echo '  pwt init                                      .worktreelinks を生成'
    echo '  pwt sync [-f|--force]                         シンボリックリンクを再同期（カレント worktree）'
    echo '                                                -f: [copy] 対象に実ファイル/ディレクトリがあれば削除して上書き'
    echo '  pwt unsync                                    シンボリックリンクを全削除（カレント worktree）'
    echo '  pwt help                                      このヘルプを表示'
    echo ''
    echo '初回セットアップ:'
    echo '  cd /path/to/project'
    echo '  pwt init                                          .worktreelinks を生成・編集'
    echo '  pwt switch -c -b feature/my-task my-task main     新規ブランチで worktree を作成して移動'
    echo ''
    echo 'ライブラリ更新（このworktreeのみ）:'
    echo '  vim .worktreelinks          該当パターンをコメントアウト'
    echo '  pwt sync                    シンボリックリンクを外す'
    echo '  npm install                 このworktreeに実体をインストール'
    echo ''
    echo '環境変数:'
    echo '  GIT_PARALLEL_WORKTREES_BASE    worktree を配置するベースディレクトリ（デフォルト: リポジトリの親）'
    echo '                                 絶対パスかつ既存ディレクトリである必要があります'
    echo ''
    echo 'Git 設定:'
    echo '  git config pwt.worktreeDir <dir>           worktree をまとめるサブディレクトリ名'
    echo '  git config --global pwt.worktreeDir <dir>  全リポジトリに適用'
    echo '                                             例: git config pwt.worktreeDir ".worktrees"'
    echo '                                             → {ベース}/.worktrees/{project}--{branch} に配置'
}

# =============================================================================
# Tab 補完
# =============================================================================

_pwt_completion_wt_targets() {
    local project_root="${1:-}"
    [ -z "$project_root" ] && return
    local i=0 wt_path branch
    while IFS=$'\t' read -r wt_path branch; do
        [ -z "$wt_path" ] && continue
        printf '%s\n' "$i"
        [ "$branch" != "detached" ] && printf '%s\n' "$branch"
        i=$((i + 1))
    done < <(_pwt_parse_worktrees "$project_root")
}

_pwt_completion_wt_branches() {
    local project_root="${1:-}"
    [ -z "$project_root" ] && return
    local wt_path branch
    while IFS=$'\t' read -r wt_path branch; do
        [ "$wt_path" != "$project_root" ] && [ "$branch" != "detached" ] \
            && printf '%s\n' "$branch"
    done < <(_pwt_parse_worktrees "$project_root")
}

_pwt_completions() {
    local subcmd_list=(switch add list remove init sync unsync help)
    local project_root
    project_root="$(_pwt_project_root)"

    if [ -n "$ZSH_VERSION" ]; then
        case "$CURRENT" in
            2)
                compadd -- "${subcmd_list[@]}"
                ;;
            3)
                case "${words[2]}" in
                    switch)
                        local wt_targets=("${(f)$(_pwt_completion_wt_targets "$project_root")}")
                        compadd -- -c "${wt_targets[@]}"
                        ;;
                    add)
                        # <path> はユーザー定義名のため補完不可。フラグのみ提示する
                        compadd -- -b -B --detach --force
                        ;;
                    remove)
                        local worktrees=("${(f)$(_pwt_completion_wt_branches "$project_root")}")
                        compadd -- "${worktrees[@]}"
                        ;;
                    sync)
                        compadd -- -f --force
                        ;;
                esac
                ;;
        esac
    elif [ -n "$BASH_VERSION" ]; then
        local cur="${COMP_WORDS[COMP_CWORD]}"
        case "$COMP_CWORD" in
            1)
                COMPREPLY=()
                local w
                for w in "${subcmd_list[@]}"; do
                    [[ -z "$cur" || "$w" == "$cur"* ]] && COMPREPLY+=("$w")
                done
                ;;
            2)
                case "${COMP_WORDS[1]}" in
                    switch)
                        local wt_targets=()
                        mapfile -t wt_targets < <(_pwt_completion_wt_targets "$project_root")
                        COMPREPLY=()
                        for w in -c "${wt_targets[@]}"; do
                            [[ -z "$cur" || "$w" == "$cur"* ]] && COMPREPLY+=("$w")
                        done
                        ;;
                    add)
                        # <path> はユーザー定義名のため補完不可。フラグのみ提示する
                        COMPREPLY=()
                        for w in -b -B --detach --force; do
                            [[ -z "$cur" || "$w" == "$cur"* ]] && COMPREPLY+=("$w")
                        done
                        ;;
                    remove)
                        local worktrees=()
                        mapfile -t worktrees < <(_pwt_completion_wt_branches "$project_root")
                        COMPREPLY=()
                        for w in "${worktrees[@]}"; do
                            [[ -z "$cur" || "$w" == "$cur"* ]] && COMPREPLY+=("$w")
                        done
                        ;;
                    sync)
                        COMPREPLY=()
                        for w in -f --force; do
                            [[ -z "$cur" || "$w" == "$cur"* ]] && COMPREPLY+=("$w")
                        done
                        ;;
                esac
                ;;
        esac
    fi
}

if [ -n "$ZSH_VERSION" ]; then
    compdef _pwt_completions pwt
elif [ -n "$BASH_VERSION" ]; then
    complete -F _pwt_completions pwt
fi
