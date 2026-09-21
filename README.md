# pwt - Git Parallel Worktrees

`git worktree` の薄いラッパー。引数体系は `git worktree add` と互換で、加えてバレネームの `<path>` を共通の置き場所に配置する利便性と、`.worktreelinks` によるシンボリックリンク/コピー同期を提供する。

## 設計方針

pwt は **pure な git worktree のラッパー** に徹する。CLI 構文・挙動は `git worktree` に合わせ、独自の引数体系（ブランチ名から自動でディレクトリ名を導出する等）は持たない。pwt の付加価値は次の 3 つに限定する:

- バレネームの `<path>` を `work_base` 配下に配置する補助
- worktree への移動 (`pwt switch` / `pwt path`)
- `.worktreelinks` を使ったシンボリックリンク/コピー同期 (`pwt sync` / `unsync`)

## インストール

### Homebrew

```bash
brew tap kaito0079/tap
brew install pwt
```

### go install

```bash
go install github.com/kaito0079/git-parallel-worktrees/cmd/pwt@latest
```

### ソースからビルド

```bash
git clone https://github.com/kaito0079/git-parallel-worktrees
cd git-parallel-worktrees
go build -o pwt ./cmd/pwt
```

## シェル統合（任意）

**pwt はこの設定なしでも全機能が使える。** 設定すると `pwt switch` でカレントディレクトリを移動できるようになる。

子プロセスは親シェルの作業ディレクトリを変更できないため、`cd` を伴う移動にはシェル関数が必要になる。`shell/pwt.sh` がそれで、`~/.zshrc`（または `~/.bashrc`）に 1 行足すと有効になる。

```bash
# Homebrew の場合
echo 'source "$(brew --prefix)/share/pwt/pwt.sh"' >> ~/.zshrc

# 手動配置の場合
echo 'source /path/to/shell/pwt.sh' >> ~/.zshrc
```

読み込まない場合は `pwt path` で移動できる:

```bash
cd "$(pwt path 2)"
```

### 補完

```bash
pwt completion zsh  > "${fpath[1]}/_pwt"
pwt completion bash > /usr/local/etc/bash_completion.d/pwt
```

## 初回セットアップ

```bash
cd /path/to/your-project

# 1. .worktreelinks を生成（.gitignore の内容をもとに作成される）
pwt init

# 2. リンクしたいパターンのコメントを外す
vim .worktreelinks

# 3. 最初の worktree を作成して移動
#    -b で新規ブランチ、my-task はディレクトリ名（バレネーム）、main は基点
pwt switch -c -b feature/my-task my-task main
```

## ファイル共有（`.worktreelinks`）

`.worktreelinks` に書いたパターンのファイル/ディレクトリが、main リポジトリからシンボリックリンクまたはコピーされる。

```
# .worktreelinks の例
# デフォルトはシンボリックリンク（一元管理向き）
.env
.env.*
docker-compose.override.yml
.claude/settings.local.json

# [copy] セクション以降はコピー（Docker 等でシンボリックリンクが使えない場合）
[copy]
vendor/
node_modules/

# [link] で再びシンボリックリンクモードに戻せる
[link]
.docker/
```

パターンの解釈は `git ls-files --exclude-from` に委譲している。書式は `.gitignore` と同じで、pwt 側では独自のマッチングを行わない。

### シンボリックリンク vs コピー

| | シンボリックリンク（デフォルト） | コピー（`[copy]`） |
|---|---|---|
| 用途 | 一元管理したいもの（`.env`） | 独立して動く必要があるもの（`vendor/`） |
| 速度 | 瞬時 | ファイル数に比例 |
| 変更の反映 | 即時（実体は1つ） | されない（独立コピー） |
| Docker | 参照先がコンテナ外で壊れる | 問題なし |

### 基本ルール

- `pwt add` で worktree を作成すると自動的にリンク/コピーが設定される
- **各 worktree は独立した `.worktreelinks` のコピーを持つ** → worktree ごとに個別設定が可能
- `.worktreelinks` をコミットすればチームで設定を共有できる

### `pwt sync` の挙動

`pwt sync` は既存のシンボリックリンクをすべて削除してから貼り直す。`[copy]` の対象は、宛先に実ファイル/実ディレクトリがある場合はスキップされる。

上書きしたい場合は `-f` を付ける:

```bash
pwt sync -f        # [copy] 対象の実体を削除して再コピーする
```

### ライブラリ更新が必要なとき

特定の worktree だけライブラリを更新したい場合:

```bash
# 1. その worktree の .worktreelinks を編集
vim .worktreelinks      # node_modules をコメントアウト

# 2. シンボリックリンクを再生成（node_modules のリンクが外れる）
pwt sync

# 3. このworktreeだけ実体をインストール
npm install

# 他の worktree は影響なし（シンボリックリンクのまま）
```

全シンボリックリンクをまとめて外したい場合:

```bash
pwt unsync
```

## コマンド一覧

`pwt add` は `git worktree add` と同じ引数体系。

| コマンド | 説明 |
|---------|------|
| `pwt` | worktree 一覧（番号付き・現在位置マーク） |
| `pwt list` | worktree 一覧（明示的） |
| `pwt path <番号\|名前>` | worktree の絶対パスのみを出力 |
| `pwt switch <番号\|名前>` | worktree に移動（シェル統合が必要） |
| `pwt switch -c [-b <branch>] [-B <branch>] [--detach] <path> [<commit-ish>]` | worktree を作成して移動 |
| `pwt add [-b <branch>] [-B <branch>] [--detach] <path> [<commit-ish>]` | worktree を作成（移動しない） |
| `pwt remove <branch\|name\|.>` | worktree を削除（ブランチ名/ディレクトリ名/`.`=カレント） |
| `pwt init` | `.worktreelinks` を生成 |
| `pwt sync [-f]` | カレント worktree のリンク/コピーを再同期 |
| `pwt unsync` | カレント worktree のシンボリックリンクを全削除 |
| `pwt completion <shell>` | 補完スクリプトを出力 |

`pwt remove` は、対象が部分一致で決まった場合と未コミットの変更がある場合に確認を求める。

### 使用例

```bash
# 新規ブランチ + 任意のディレクトリ名（チケット名のブランチを review_1 などで管理したいとき）
pwt add -b feature/PROJ-123 review_1 main

# 既存ブランチをチェックアウト（チェックアウト先のディレクトリ名を指定）
pwt add review_1 feature/PROJ-123

# ディレクトリ名と同名のブランチを新規作成（git worktree のデフォルト挙動）
pwt add hotfix

# `/` 含みブランチを 1 引数で扱う（auto branch mode）
# ディレクトリ・ブランチともに feature/PROJ-123 になる
pwt switch -c feature/PROJ-123
```

### `<path>` の解釈

- **バレネーム**（`/` を含まない、例: `review_1`）→ `work_base` 配下に配置（`pwt.worktreePrefix` 設定を反映）
- **`/` を含む or 絶対パス** → そのまま `git worktree add` に渡す

### auto branch mode

`<path>` が絶対パスや相対パス明示 (`./foo`, `../foo`) でなく、`-b` / `-B` / `--detach` および `<commit-ish>` がいずれも未指定のときは、`<path>` をブランチ名として自動推論する (bare name / `/` 含みのどちらでも有効):

| 状態 | 挙動 |
|------|------|
| `refs/heads/<path>` が存在 | そのブランチをチェックアウト |
| `refs/remotes/origin/<path>` のみ存在 | 同名 local ブランチを origin 追従で作成 |
| どこにも無い | HEAD ベースで新規ブランチを作成 |

どのケースで解決したかは実行時に 1 行表示される。worktree のディレクトリは `<path>` をそのまま使い (`work_base/<path>`)、ブランチ名と一致する。

明示的にディレクトリとブランチを分けたい場合は `-b` を使う:

```bash
pwt switch -c -b feature/PROJ-123 review_1 main   # ブランチ feature/PROJ-123 / ディレクトリ review_1
```

## ナビゲーション

```bash
pwt                      # worktree 一覧（番号付き・現在位置 > マーク）
pwt switch 2             # 番号で移動
pwt switch feature       # ブランチ名の部分一致で移動
```

表示例:

```
=== myapp ===
  > 0  /repos/myapp                  (main)
    1  /repos/myapp--review_1        (feature/PROJ-123)
    2  /repos/myapp--hotfix          (hotfix)
```

## ディレクトリ命名規則

`pwt add <path>` の `<path>` がバレネーム（`/` を含まない）のとき、pwt が `work_base` 配下に配置する。デフォルトでは main リポジトリの隣:

```
/repos/myapp/                      ← main リポジトリ
/repos/myapp--review_1/            ← worktree (path: review_1)
/repos/myapp--hotfix/              ← worktree (path: hotfix)
```

### 配置先の変更

`pwt.worktreeDir` で worktree の配置先とディレクトリ命名を切り替えられる:

| 設定 | 配置例 (`pwt add review_1` のとき) | prefix |
|---|---|---|
| 未設定 | `/repos/myapp--review_1/` | `<repo>--<path>` |
| `.worktrees` | `/repos/.worktrees/myapp--review_1/` | `<repo>--<path>` |
| `./.worktrees` | `/repos/myapp/.worktrees/review_1/` | `<path>` のみ |

```bash
# 親ディレクトリ配下の専用サブディレクトリにまとめる
git config pwt.worktreeDir .worktrees

# main リポジトリ内に配置する（プロジェクトごとに完結）
git config pwt.worktreeDir ./.worktrees
```

### prefix の明示指定

`pwt.worktreePrefix` で `<repo>--` prefix の付与を強制できる:

| 値 | 挙動 |
|---|---|
| `auto` (既定) | 配置先から推論（`./X` なら付けない、それ以外は付ける） |
| `repo` | 常に `<repo>--<slug>` |
| `none` | 常に `<slug>` のみ |

```bash
# main repo 内配置でも <repo>-- を付けたい場合
git config pwt.worktreeDir ./.worktrees
git config pwt.worktreePrefix repo
# → /repos/myapp/.worktrees/myapp--feature-auth/
```

### 環境変数での上書き

```bash
export GIT_PARALLEL_WORKTREES_BASE=/path/to/worktrees
```

絶対パスかつ既存のディレクトリである必要がある。

## 動作要件

- **OS**: macOS / Linux
- **依存**: git
- シェル統合を使う場合: bash / zsh

## 開発

```bash
go test ./...
go vet ./...
gofmt -l .
```

リリース:

```bash
goreleaser release --clean     # バイナリのビルドと GitHub Release の作成
go run ./tools/formula         # Homebrew formula を生成（dist/Formula/pwt.rb）
```

`internal/links` と `internal/cli` のテストは実際の git を呼ぶ統合テストを含む。

## ライセンス

MIT
