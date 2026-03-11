# pwt - Git Parallel Worktrees

Git worktree をブランチ単位でオンデマンドに作成・管理するシェルツール。
ライブラリ（node_modules, vendor 等）をシンボリックリンクにすることで、新規 worktree で即作業開始できる。

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

# 3. 最初の worktree を作成
pwt new feature/my-task
```

## シンボリックリンク（`.worktreelinks`）

`.worktreelinks` に書いたパターンのファイル/ディレクトリが、main リポジトリからシンボリックリンクされる。

```
# .worktreelinks の例
node_modules/
vendor/
.env
.env.*
docker-compose.override.yml
.claude/settings.local.json
```

- `pwt new` で worktree を作成すると自動的にシンボリックリンクが設定される
- **各 worktree は独立した `.worktreelinks` のコピーを持つ** → worktree ごとに個別設定が可能
- `.worktreelinks` をコミットすればチームで設定を共有できる

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
| `pwt <番号>` | 番号で worktree に移動 |
| `pwt <名前>` | ブランチ名/ディレクトリ名の部分一致で移動 |
| `pwt init` | `.worktreelinks` を生成 |
| `pwt new <branch> [--from <base>]` | worktree を作成（ブランチ作成・symlink設定・cd まで自動） |
| `pwt list` | worktree 一覧（明示的） |
| `pwt rm <branch>` | worktree を削除 |
| `pwt sync` | カレント worktree のシンボリックリンクを再同期 |
| `pwt unsync` | カレント worktree のシンボリックリンクを全削除 |
| `pwt help` | ヘルプを表示 |

## ナビゲーション

```bash
pwt              # worktree 一覧（番号付き・現在位置 > マーク）
pwt 2            # 番号で移動
pwt feature      # ブランチ名の部分一致で移動
```

表示例:

```
=== myapp ===
  > 0  /repos/myapp                         (main)
    1  /repos/myapp--feature-auth           (feature/auth)
    2  /repos/myapp--fix-login              (fix/login)
```

## ディレクトリ命名規則

worktree は main リポジトリの隣に配置される:

```
/repos/myapp/                      ← main リポジトリ
/repos/myapp--feature-auth/        ← worktree (branch: feature/auth)
/repos/myapp--fix-login/           ← worktree (branch: fix/login)
```

配置先を変更する場合:

```bash
export GIT_PARALLEL_WORKTREES_BASE=/path/to/worktrees
```

## 動作要件

- **シェル**: bash 4.0+ / zsh 5.0+
- **OS**: macOS / Linux (Ubuntu)
- **依存**: git
