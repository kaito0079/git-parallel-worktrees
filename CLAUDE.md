# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト概要

pwt (Git Parallel Worktrees) は `git worktree` のラッパーとなる CLI ツール。worktree の作成・切り替え・削除と、共有アセット（node_modules, vendor 等）のシンボリックリンク管理を提供する。Go で実装し、単一バイナリとして配布する。

## 設計方針

- **pure な git worktree のラッパーに徹する**: pwt は `git worktree` の薄いラッパーであり、独自の流儀を持ち込まない。
  - **CLI 構文を git worktree に合わせる**: 引数の並び・フラグ名・意味は `git worktree add/remove/list` と一致させる。例として `pwt add` は `git worktree add [-b <new-branch>] [-B <new-branch>] [--detach] <path> [<commit-ish>]` の構文に従う。pwt 独自の引数体系（ブランチ名から自動でディレクトリ名を導出する等）は取らない。
  - **挙動も git worktree に委譲**: 強制削除・権限問題の自動解決・ブランチ作成の暗黙ルールなど、git worktree が扱う領域に上乗せの管理処理は加えない。失敗時はエラーを伝え、対処はユーザーに委ねる。
  - **pwt の付加価値は限定**: worktree 間の移動 (`switch` / `path`)、シンボリックリンク同期 (`sync`/`unsync`)、`.worktreelinks` 管理に限る。
- **シェル統合は opt-in**: 子プロセスは親シェルの作業ディレクトリを変更できないため、`cd` を伴う移動にはシェル関数 (`shell/pwt.sh`) が必要になる。これを必須にはしない。バイナリ単体で全機能が使え、`cd "$(pwt path 2)"` で移動できる状態を保つ。
- **eval を使わない**: シェル関数への移動先の受け渡しは、`PWT_CD_FILE` が指す一時ファイル経由で行う。コードインジェクションを防ぐため、シェルに評価させる文字列は渡さない。
- **git にパターンマッチを委譲**: `.worktreelinks` は gitignore 形式で、`git ls-files --exclude-from` で処理する。gitignore のセマンティクスを Go 側で再実装しない。

## テスト

```bash
go test ./...
go vet ./...
gofmt -l .
```

`internal/links` と `internal/cli` には実際の git を呼ぶ統合テストが含まれる。外部サービスへの依存はない。

## アーキテクチャ

```
cmd/pwt/            エントリポイント
internal/cli/       サブコマンドの定義と実行ロジック (cobra)
internal/repo/      リポジトリ/worktree の情報とパス解決
internal/links/     .worktreelinks の解釈とリンク/コピー同期
internal/gitcmd/    git 実行の抽象化 (Runner interface, Exec, Fake)
shell/pwt.sh        シェル統合 (opt-in)
```

**主要な型と関数:**
- `repo.Context` — `ProjectRoot` / `ProjectName` / `WorkBase` / `UsePrefix`。`repo.ResolveContext` が環境変数と git config から組み立てる。ディレクトリは作らない（作成は `add` 側）
- `repo.ParseWorktrees` — `git worktree list --porcelain` をパース
- `repo.WorktreePath` / `repo.ResolveAddPath` — `<path>` 引数から配置先を決める
- `links.Parse` / `links.Sync` / `links.CleanSymlinks` — `.worktreelinks` の分割と同期
- `gitcmd.Runner` — git 実行の interface。テストでは `gitcmd.Fake` に差し替える

**ディレクトリ配置 (`pwt add <path>` の `<path>` 解釈):**
- バレネーム（`/` を含まない）: `{work_base}/{project_name}--<path>` または `{work_base}/<path>` に配置（`pwt.worktreePrefix` 設定による）。例: `pwt add review_1` → `/repos/.worktrees/myapp--review_1/`
- `/` を含む or 絶対パス: そのまま `git worktree add <path>` に渡す（git worktree と同じ挙動）

## 言語規約

- Go（要求バージョンは `go.mod` の go ディレクティブを参照）
- **コメントは日本語、ユーザー向けの出力（エラー・ヘルプ・ログ）は英語**
- 入力検証: `git check-ref-format` でブランチ名、フラグインジェクション（`-` 始まり）・パストラバーサル（`..`）のチェック
- 終了コード: 0 = 正常 / 1 = 実行時エラー / 2 = 引数の誤り

## リリース

`.goreleaser.yaml` でタグ push からバイナリのビルドと Homebrew tap (`kaito0079/homebrew-tap`) の formula 更新までを行う。
