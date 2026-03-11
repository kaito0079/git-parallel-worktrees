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
#   pwt switch -c <branch> [--from <b>]       worktree を作成して移動
#   pwt add <branch> [--from <b>]             worktree を作成（移動しない）
#   pwt list                                  worktree 一覧（明示的）
#   pwt remove <branch>                       worktree を削除
#   pwt init                                  .worktreelinks を生成
#   pwt sync                                  シンボリックリンクを再同期
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

# ブランチ名をディレクトリスラグに変換（/ → -）
_pwt_branch_slug() {
    printf '%s\n' "${1//\//-}"
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
    local wt_path="" branch="" detached=false
    while IFS= read -r line; do
        if [[ "$line" == "worktree "* ]]; then
            if [ -n "$wt_path" ]; then
                if [ "$detached" = "true" ]; then
                    printf '%s\tdetached\n' "$wt_path"
                else
                    printf '%s\t%s\n' "$wt_path" "${branch:-detached}"
                fi
            fi
            wt_path="${line#worktree }"
            branch=""
            detached="false"
        elif [[ "$line" == "branch "* ]]; then
            local raw_branch="${line#branch }"
            branch="${raw_branch#refs/heads/}"
        elif [[ "$line" == "detached" ]]; then
            detached="true"
        fi
    done < <(git -C "$root" worktree list --porcelain 2>/dev/null)
    if [ -n "$wt_path" ]; then
        if [ "$detached" = "true" ]; then
            printf '%s\tdetached\n' "$wt_path"
        else
            printf '%s\t%s\n' "$wt_path" "${branch:-detached}"
        fi
    fi
}

# project_root / project_name / work_base をタブ区切りで stdout に出力
# eval を使わず stdout 返却方式とすることでコードインジェクションを防ぐ
# 使い方:
#   local _ctx
#   _ctx="$(_pwt_resolve_context)" || return 1
#   IFS=$'\t' read -r project_root project_name work_base <<< "$_ctx"
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
    # 設定例: git config pwt.worktreeDir ".worktrees" → {base}/.worktrees/ 配下に作成
    local wt_dir
    wt_dir="$(git -C "$root" config --get pwt.worktreeDir 2>/dev/null || true)"
    if [ -n "$wt_dir" ]; then
        if [[ "$wt_dir" == /* ]] || [[ "$wt_dir" == *..* ]]; then
            echo "エラー: pwt.worktreeDir は相対パス（サブディレクトリ名）で指定してください" >&2
            return 1
        fi
        base="${base%/}/${wt_dir}"
        if [ ! -d "$base" ]; then
            mkdir -p "$base" || {
                echo "エラー: ディレクトリの作成に失敗しました: $base" >&2
                return 1
            }
        fi
    fi

    printf '%s\t%s\t%s' "$root" "${root##*/}" "$base"
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

    {
        echo '# .worktreelinks — worktree にシンボリックリンクするファイル/ディレクトリのパターン'
        echo '#'
        echo '# リンクしたいパターンのコメント (#) を外してください。'
        echo '# パターンは .gitignore と同じルールで解釈されます:'
        echo '#   .env            どの階層でもマッチ (/ を含まないパターン)'
        echo '#   path/to/file    ルートからの相対パス (/ を含むパターン)'
        echo '#   *.log           ワイルドカード'
        echo '#'
        echo '# [ライブラリ (node_modules, vendor 等) について]'
        echo '# シンボリックリンクにすると新しい worktree で即作業開始できます。'
        echo '# ライブラリ更新が必要な worktree では:'
        echo '#   1. このファイルから該当パターンをコメントアウト'
        echo '#   2. pwt sync  → シンボリックリンクが外れる'
        echo '#   3. npm install 等で実体をインストール'
        echo '#   他の worktree には影響しません。'
        echo '#'
        echo '# [git 管理について]'
        echo '# このファイルをコミットするとチームで設定を共有できます。'
        echo '# gitignore に追加した場合も、worktree 作成時に自動でコピーされます。'
        echo '# いずれの場合も各 worktree が独立したコピーを持ちます。'
        echo '#'
        echo '# [制約]'
        echo '# リンク対象は git ls-files --others --ignored で列挙されるファイル/ディレクトリに限ります。'
        echo '# つまり、メインリポジトリの .gitignore（または .git/info/exclude）で無視されているものが対象です。'
        echo ''
    } > "$config"

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

    while IFS= read -r -d '' link; do
        local raw_target
        raw_target=$(readlink "$link" 2>/dev/null) || continue

        local resolved
        resolved=$(_pwt_realpath "$raw_target" "${link%/*}")
        [ -z "$resolved" ] && resolved="$raw_target"

        if [[ "$resolved" == "$src_root/"* ]] || [[ "$resolved" == "$src_root" ]]; then
            rm -f "$link"
            count=$((count + 1))
        fi
    done < <(find "$dest_root" -maxdepth 50 -type l -not -path "*/.git/*" -print0 2>/dev/null)

    if [ "$count" -gt 0 ]; then echo "  ${count} 個のシンボリックリンクを削除"; fi
}

# .worktreelinks のパターンに従いシンボリックリンクを作成
# git ls-files --exclude-from で git 自身にパターンマッチを委譲する
_pwt_create_symlinks() {
    local src_root="$1" dest_root="$2"
    local config="$dest_root/.worktreelinks"

    if [ ! -f "$config" ]; then
        echo "  (.worktreelinks が見つかりません)"
        return
    fi

    # 既存のシンボリックリンクを削除
    _pwt_clean_symlinks "$src_root" "$dest_root"

    # .worktreelinks に有効なパターンがあるか簡易チェック
    local has_pattern=false
    while IFS= read -r line; do
        [[ -z "$line" || "$line" == \#* ]] && continue
        has_pattern=true
        break
    done < "$config"

    if [ "$has_pattern" = false ]; then
        echo "  (.worktreelinks にパターンがありません)"
        return
    fi

    # git ls-files --exclude-from で git にパターンマッチを委譲
    # --others: 追跡されていないファイル
    # --ignored: 無視されているもの（--exclude-from で指定）
    # --directory: ディレクトリ単位で返す（ディレクトリ内を再帰しない）
    # -z: NUL 区切り出力
    local count=0
    while IFS= read -r -d '' raw_entry; do
        local entry="${raw_entry%/}"
        [ -z "$entry" ] && continue

        # パストラバーサル防止（多層防御）
        if [[ "$entry" =~ (^|/)\.\.(/|$) ]]; then
            echo "  [!] 不正なパスをスキップ: $entry" >&2
            continue
        fi

        local src="$src_root/$entry"
        local dest="$dest_root/$entry"

        [ -e "$src" ] || continue

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
        if [ -d "$src" ]; then
            echo "  [リンク] $entry/"
        else
            echo "  [リンク] $entry"
        fi
    done < <(git -C "$src_root" ls-files -z \
        --others --ignored --exclude-from="$config" --directory 2>/dev/null)

    [ "$count" -eq 0 ] && echo "  (リンク対象がありません)" \
                       || echo "  ${count} 個のシンボリックリンクを作成"
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
    local project_root project_name work_base
    IFS=$'\t' read -r project_root project_name work_base <<< "$_ctx"

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
_pwt_cmd_switch() {
    local create=false target="" args_for_add=()

    while [ "$#" -gt 0 ]; do
        case "$1" in
            -c)
                create=true
                shift
                if [ -z "${1:-}" ]; then
                    echo "エラー: -c にはブランチ名が必要です" >&2
                    echo "使い方: pwt switch -c <branch> [--from <base>]" >&2
                    return 1
                fi
                target="$1"
                ;;
            --from)
                args_for_add+=("--from")
                shift
                if [ -z "${1:-}" ]; then
                    echo "エラー: --from には値が必要です" >&2
                    return 1
                fi
                args_for_add+=("$1")
                ;;
            *)
                if [ -n "$target" ]; then
                    echo "エラー: 余分な引数: $1" >&2
                    return 1
                fi
                target="$1"
                ;;
        esac
        shift
    done

    if [ -z "$target" ]; then
        echo "使い方: pwt switch <番号|名前>" >&2
        echo "        pwt switch -c <branch> [--from <base>]" >&2
        return 1
    fi

    # -c: 作成して移動
    if [ "$create" = true ]; then
        _pwt_cmd_add "$target" "${args_for_add[@]}" || return 1
        # add が成功したら移動
        _pwt_navigate "$target"
        return $?
    fi

    # 既存 worktree に移動
    _pwt_navigate "$target"
}

# worktree に cd する内部ヘルパー
_pwt_navigate() {
    local target="$1"

    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base
    IFS=$'\t' read -r project_root project_name work_base <<< "$_ctx"

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
# add
_pwt_cmd_add() {
    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base
    IFS=$'\t' read -r project_root project_name work_base <<< "$_ctx"

    local branch="" base=""
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --from)
                shift
                if [ -z "${1:-}" ]; then
                    echo "エラー: --from には値が必要です" >&2
                    echo "使い方: pwt add <branch> [--from <base>]" >&2
                    return 1
                fi
                base="$1"
                ;;
            *)
                if [ -n "$branch" ]; then
                    echo "エラー: 余分な引数: $1" >&2
                    echo "使い方: pwt add <branch> [--from <base>]" >&2
                    return 1
                fi
                branch="$1"
                ;;
        esac
        shift
    done

    if [ -z "$branch" ]; then
        echo "使い方: pwt add <branch> [--from <base>]" >&2
        return 1
    fi

    # --from 省略時は現在の worktree の HEAD を基点にする（git worktree add と同じ挙動）
    if [ -z "$base" ]; then
        base="$(git rev-parse HEAD 2>/dev/null)" || {
            echo "エラー: 現在の HEAD を解決できません" >&2
            return 1
        }
    fi

    _pwt_validate_branch "$branch" || return 1

    # base のバリデーション（rev-parse 済みの SHA でなければチェック）
    if ! [[ "$base" =~ ^[0-9a-fA-F]{40,64}$ ]]; then
        if [[ "$base" == -* ]]; then
            echo "エラー: --from の値が '-' で始まっています: $base" >&2
            return 1
        fi
        if ! [[ "$base" =~ ^[0-9a-fA-F]{7,64}$ ]]; then
            if ! git check-ref-format "$base" >/dev/null 2>&1 \
            && ! git check-ref-format --allow-onelevel "$base" >/dev/null 2>&1; then
                echo "エラー: 不正な --from 値: $base" >&2
                return 1
            fi
        fi
        if ! git -C "$project_root" rev-parse --verify "$base" >/dev/null 2>&1; then
            echo "エラー: --from '$base' が存在しません（ref または commit SHA を指定してください）" >&2
            return 1
        fi
    fi

    local slug wt_path
    slug=$(_pwt_branch_slug "$branch")
    wt_path="${work_base%/}/${project_name}--${slug}"
    wt_path="${wt_path%/}"

    if [ -d "$wt_path" ]; then
        echo "エラー: '$wt_path' は既に存在します" >&2
        echo "  ブランチ名が異なってもスラグが衝突する場合があります (/ → - 変換)" >&2
        return 1
    fi

    echo "=== Worktree 作成: $branch ==="

    local branch_created=""
    if ! git -C "$project_root" show-ref --verify --quiet "refs/heads/$branch" 2>/dev/null; then
        if git -C "$project_root" show-ref --verify --quiet "refs/remotes/origin/$branch" 2>/dev/null; then
            if ! git -C "$project_root" branch --track -- "$branch" "origin/$branch"; then
                echo "エラー: ブランチの作成に失敗しました: $branch (tracking origin/$branch)" >&2
                return 1
            fi
            echo "  [+] ブランチ作成: $branch (tracking origin/$branch)"
        else
            if ! git -C "$project_root" branch -- "$branch" "$base"; then
                echo "エラー: ブランチの作成に失敗しました: $branch (from $base)" >&2
                return 1
            fi
            echo "  [+] ブランチ作成: $branch (from $base)"
        fi
        branch_created="1"
    fi

    if ! git -C "$project_root" worktree add -- "$wt_path" "$branch" >/dev/null; then
        echo "エラー: worktree の作成に失敗しました" >&2
        echo "  ブランチが既に他の worktree でチェックアウトされている可能性があります" >&2
        if [ -n "$branch_created" ]; then
            git -C "$project_root" branch -D -- "$branch" 2>/dev/null \
                && echo "  [ロールバック] ブランチを削除しました: $branch" >&2
        fi
        return 1
    fi
    echo "  [+] worktree 作成: $wt_path"

    if [ ! -f "$wt_path/.worktreelinks" ]; then
        if [ -f "$project_root/.worktreelinks" ]; then
            cp "$project_root/.worktreelinks" "$wt_path/.worktreelinks"
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
    echo "  移動: pwt switch $branch"
}

# ----------------------------------------------------------------
# remove
_pwt_cmd_remove() {
    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base
    IFS=$'\t' read -r project_root project_name work_base <<< "$_ctx"

    local branch="${1:-}"
    if [ -z "$branch" ]; then
        echo "使い方: pwt remove <branch>" >&2
        return 1
    fi

    _pwt_validate_branch "$branch" || return 1

    local slug wt_path
    slug=$(_pwt_branch_slug "$branch")
    wt_path="${work_base%/}/${project_name}--${slug}"
    wt_path="${wt_path%/}"

    if [ ! -d "$wt_path" ]; then
        echo "エラー: worktree が見つかりません: $wt_path" >&2
        return 1
    fi

    if ! git -C "$project_root" worktree list --porcelain 2>/dev/null \
            | grep -Fxq "worktree $wt_path"; then
        echo "エラー: '$wt_path' は git worktree として登録されていません" >&2
        echo "  git worktree list で確認してください" >&2
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
            return 1
        fi
    else
        if ! git -C "$project_root" worktree remove -- "$wt_path"; then
            echo "エラー: worktree の削除に失敗しました: $wt_path" >&2
            return 1
        fi
    fi

    echo "削除しました: $wt_path"
}

# ----------------------------------------------------------------
# sync
_pwt_cmd_sync() {
    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base
    IFS=$'\t' read -r project_root project_name work_base <<< "$_ctx"

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
    _pwt_create_symlinks "$project_root" "$current_root"
}

# ----------------------------------------------------------------
# unsync
_pwt_cmd_unsync() {
    local _ctx
    _ctx="$(_pwt_resolve_context)" || return 1
    local project_root project_name work_base
    IFS=$'\t' read -r project_root project_name work_base <<< "$_ctx"

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
    local project_root project_name work_base
    IFS=$'\t' read -r project_root project_name work_base <<< "$_ctx"

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
    echo '  pwt switch -c <branch> [--from <base>]        worktree を作成して移動'
    echo '  pwt add <branch> [--from <base>]              worktree を作成（移動しない）'
    echo '  pwt list                                      worktree 一覧（明示的）'
    echo '  pwt remove <branch>                           worktree を削除'
    echo '  pwt init                                      .worktreelinks を生成'
    echo '  pwt sync                                      シンボリックリンクを再同期（カレント worktree）'
    echo '  pwt unsync                                    シンボリックリンクを全削除（カレント worktree）'
    echo '  pwt help                                      このヘルプを表示'
    echo ''
    echo '初回セットアップ:'
    echo '  cd /path/to/project'
    echo '  pwt init                            .worktreelinks を生成・編集'
    echo '  pwt switch -c feature/my-task       worktree を作成して移動'
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

_pwt_completion_branches() {
    local project_root="${1:-}"
    [ -z "$project_root" ] && return
    git -C "$project_root" branch --format='%(refname:short)' 2>/dev/null
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
                        local branches=("${(f)$(_pwt_completion_branches "$project_root")}")
                        compadd -- "${branches[@]}"
                        ;;
                    remove)
                        local worktrees=("${(f)$(_pwt_completion_wt_branches "$project_root")}")
                        compadd -- "${worktrees[@]}"
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
                        local branches=()
                        mapfile -t branches < <(_pwt_completion_branches "$project_root")
                        COMPREPLY=()
                        for w in "${branches[@]}"; do
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
