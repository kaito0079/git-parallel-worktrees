# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト概要

pwt (Git Parallel Worktrees) は `git worktree` のラッパーとなるシェルツール。worktree の作成・切り替え・削除と、共有アセット（node_modules, vendor 等）のシンボリックリンク管理を提供する。

## 設計方針

- **pure な git worktree のラッパーに徹する**: pwt は `git worktree` の薄いラッパーであり、独自の流儀を持ち込まない。
  - **CLI 構文を git worktree に合わせる**: 引数の並び・フラグ名・意味は `git worktree add/remove/list` と一致させる。例として `pwt add` は `git worktree add [-b <new-branch>] [-B <new-branch>] [--detach] <path> [<commit-ish>]` の構文に従う。pwt 独自の引数体系（ブランチ名から自動でディレクトリ名を導出する等）は取らない。
  - **挙動も git worktree に委譲**: 強制削除・権限問題の自動解決・ブランチ作成の暗黙ルールなど、git worktree が扱う領域に上乗せの管理処理は加えない。失敗時はエラーを伝え、対処はユーザーに委ねる。
  - **pwt の付加価値は限定**: `cd` を伴う移動 (`switch`)、シンボリックリンク同期 (`sync`/`unsync`)、`.worktreelinks` 管理に限る。
- **シェル関数として動作**: `cd` で親シェルのカレントディレクトリを変更する必要があるため、外部バイナリではなくシェル関数として実装。`source pwt.sh` で読み込む。
- **eval を使わない**: コンテキスト受け渡しは stdout + タブ区切りで行い、コードインジェクションを防止する。
- **git にパターンマッチを委譲**: `.worktreelinks` は gitignore 形式で、`git ls-files --exclude-from` で処理する。自前の正規表現変換は行わない。

## テスト

```bash
bash tests/test_pwt.sh
```

モック git 関数を使った単体テスト。外部依存なし。

## アーキテクチャ

単一ファイル `pwt.sh` にすべて実装。

**関数命名規則:**
- `pwt()` - エントリポイント（サブコマンドディスパッチ）
- `_pwt_cmd_*()` - サブコマンドハンドラ（list, switch, add, remove, sync, unsync, init, help）
- `_pwt_*()` - 内部ヘルパー

**主要な内部ヘルパー:**
- `_pwt_resolve_context()` → `project_root\tproject_name\twork_base\tuse_prefix` をタブ区切りで返す
- `_pwt_parse_worktrees()` → `git worktree list --porcelain` をパースして `path\tbranch` を返す
- `_pwt_wt_path()` → `<path>` 引数がバレネームのときに `{work_base}/{project}--<name>` 等を組み立てる
- `_pwt_create_symlinks()` → `.worktreelinks` に基づきシンボリックリンク/コピーを作成

**ディレクトリ配置 (`pwt add <path>` の `<path>` 解釈):**
- バレネーム（`/` を含まない）: `{work_base}/{project_name}--<path>` または `{work_base}/<path>` に配置（`pwt.worktreePrefix` 設定による）。例: `pwt add review_1` → `/repos/.worktrees/myapp--review_1/`
- `/` を含む or 絶対パス: そのまま `git worktree add <path>` に渡す（git worktree と同じ挙動）

## 言語・シェル規約

- bash 4.0+ / zsh 5.0+ 両対応
- 日本語コメント・メッセージ
- NUL 区切り（`-z` / `read -d ''`）でファイルパスを安全に処理
- 入力検証: `git check-ref-format` でブランチ名、フラグインジェクション・パストラバーサルのチェック
