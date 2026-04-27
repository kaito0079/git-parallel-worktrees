# pwt - Git Parallel Worktrees

`git worktree` の薄いラッパーとなるシェルツール。引数体系は `git worktree add` と互換で、加えてバレネームの `<path>` を共通の置き場所に配置する利便性と、`.worktreelinks` によるシンボリックリンク/コピー同期を提供する。

## 設計方針

pwt は **pure な git worktree のラッパー** に徹する。CLI 構文・挙動は `git worktree` に合わせ、独自の引数体系（ブランチ名から自動でディレクトリ名を導出する等）は持たない。pwt の付加価値は次の 3 つに限定する:

- バレネームの `<path>` を `work_base` 配下に配置する補助
- worktree への `cd` 移動 (`pwt switch`)
- `.worktreelinks` を使ったシンボリックリンク/コピー同期 (`pwt sync` / `unsync`)

## インストール

### 手動インストール

```bash
cd ~/path/to/pwt
chmod +x install.sh
./install.sh
echo 'source "${XDG_DATA_HOME:-$HOME/.local/share}/pwt/pwt.sh"' >> ~/.zshrc
source ~/.zshrc
```

### アンインストール

```bash
./install.sh --uninstall
```

## 初回セットアップ

```bash
cd /path/to/your-project

# 1. .worktreelinks を生成（.gitignore の内容をもとに作成される）
pwt init

# 2. リンクしたいパターンのコメントを外す
vim .worktreelinks

# 3. 最初の worktree を作成して移動
pwt switch -c feature/my-task
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

`pwt sync` はシンボリックリンクのみ再作成する。**`[copy]` でコピーされたファイルは `pwt sync` では削除・再コピーされない**（既に実体が存在するためスキップされる）。

コピーを更新したい場合は、手動で削除してから再同期する:

```bash
rm -rf vendor/
pwt sync        # → main リポジトリから再コピーされる
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

| コマンド | 説明 |
|---------|------|
| `pwt` | worktree 一覧（番号付き・現在位置マーク） |
| `pwt switch <番号\|名前>` | worktree に移動 |
| `pwt switch -c <branch> [--from <base>]` | worktree を作成して移動 |
| `pwt add <branch> [--from <base>]` | worktree を作成（移動しない） |
| `pwt list` | worktree 一覧（明示的） |
| `pwt remove <branch>` | worktree を削除 |
| `pwt init` | `.worktreelinks` を生成 |
| `pwt sync` | カレント worktree のリンク/コピーを再同期 |
| `pwt unsync` | カレント worktree のシンボリックリンクを全削除 |
| `pwt help` | ヘルプを表示 |

## ナビゲーション

```bash
pwt                      # worktree 一覧（番号付き・現在位置 > マーク）
pwt switch 2             # 番号で移動
pwt switch feature       # ブランチ名の部分一致で移動
```

表示例:

```
=== myapp ===
  > 0  /repos/myapp                         (main)
    1  /repos/myapp--feature-auth           (feature/auth)
    2  /repos/myapp--fix-login              (fix/login)
```

## ディレクトリ命名規則

デフォルトでは worktree は main リポジトリの隣に配置される:

```
/repos/myapp/                      ← main リポジトリ
/repos/myapp--feature-auth/        ← worktree (branch: feature/auth)
/repos/myapp--fix-login/           ← worktree (branch: fix/login)
```

### 配置先の変更

`pwt.worktreeDir` で worktree の配置先とディレクトリ命名を切り替えられる:

| 設定 | 配置例 | prefix |
|---|---|---|
| 未設定 | `/repos/myapp--feature-auth/` | `<repo>--<slug>` |
| `.worktrees` | `/repos/.worktrees/myapp--feature-auth/` | `<repo>--<slug>` |
| `./.worktrees` | `/repos/myapp/.worktrees/feature-auth/` | `<slug>` のみ |

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

## 動作要件

- **シェル**: bash 4.0+ / zsh 5.0+
- **OS**: macOS / Linux (Ubuntu)
- **依存**: git
