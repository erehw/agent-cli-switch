# agent-cli-switch（acsw）

按项目切换 Codex 等 AI 编程 CLI 的登录源：ChatGPT 订阅和 API 一键切换，会话可接续，key 存 Windows 凭据管理器。

> 命令名 `acsw`（agent cli switch）。当前版本 0.1.0。已在 Windows 11 + Codex CLI 0.167 上实测：`threads` 表确有 `id`、`model_provider`、`cwd`、`updated_at`（秒）、`title` 列，`cwd` 可能带 `\\?\` 前缀（acsw 已兼容）；`cli_auth_credentials_store = "file"` 和 `supports_websockets = false` 在 `--strict-config` 下都被接受。

## 安装

- 直接用：把 `dist/acsw.exe` 放到 PATH 里的目录。
- 自己编译（需要 Go 1.26 或更新版本，不需要 C 编译器）：
  ```
  go build -trimpath -o dist/acsw.exe ./cmd/acsw
  ```
- 根目录默认是 `%USERPROFILE%\agent-source\codex`，可以用环境变量 `ACSW_ROOT` 改。测试时建议先设独立测试根目录，不碰正式目录。
- codex 路径：默认用 PATH 里的 `codex`（npm 装的 `codex.cmd` 也可以），也可以用 `ACSW_CODEX` 指定完整路径。

## 术语

| 名称 | 是什么 | 位置 |
|---|---|---|
| source | 一份登录或一份 API 配置，比如 plus1、pro1、api1 | `source\<名称>\` |
| provider | 服务商，只有两个固定名：`openai`（ChatGPT 订阅）和 `custom`（所有 API source） | 写进 home 的 config.toml，也记录在每条会话里 |
| home | 真正的 `CODEX_HOME`，存会话、`state_5.sqlite`、共用设置（AGENTS.md、skills 等） | `home\<名称>\` |
| map.toml | 项目目录前缀对应哪个 home，以及每个 home 当前用哪个 source | 根目录 |

- openai source：`source\plus1\auth.json`，也就是 ChatGPT 登录。
- custom source：`source\api1\acsw-source.toml` 里存 `base_url` 和 `model`；**key 存在 Windows 凭据管理器**，目标名 `acsw/codex/api1`，不写进任何文件。
- 所有 custom source 的 provider 都叫 `custom`，所以在 api1 和 api2 之间切换时不用改库；只有在 openai 和 custom 之间切换才需要改。

## 命令

```
acsw source add plus1 --type openai                      # 以 CODEX_HOME=source\plus1 运行 codex login
acsw source add pro1  --type openai --from %USERPROFILE%\.codex   # 从现有 CODEX_HOME 复制 auth.json，不显示内容
acsw source add api1  --type custom --base-url https://api.example.com/v1 --model gpt-5.x   # 提示输入 key，不回显
acsw source list
acsw home add home1
acsw home cmd home2                                      # 生成 home\home2\acsw-codex.cmd（给别的程序调用）
acsw map home1 [项目目录]                                 # 不写目录就用当前目录；子目录也归 home1，最长前缀优先
acsw use home1 plus1 [--dry-run] [--strict]
acsw codex [--home home1] [--expect-home home2] [codex 参数...]
acsw run [codex] [...]                                   # 等同 acsw codex（兼容旧用法）
acsw last [--home home1] [-n 5]
acsw status
acsw ui [--port 7799] [--open]
```

### 两种启动方式

| 方式 | 命令/文件 | 适合 | 规则 |
|---|---|---|---|
| A01 | `home\<home>\acsw-codex.cmd`（`acsw home cmd` 生成） | 给别的程序填路径 | 当前目录必须已映射到该 home，否则拒绝（退出码 2） |
| A02 | `acsw codex ...` | 自己在终端敲 | 按当前目录找 home；没映射就拒绝（退出码 2） |

- A01 的脚本内容等价于：`acsw codex --expect-home <home> %*`，并用 `@exit /b %ERRORLEVEL%` 传出退出码。脚本里写的是生成时 `acsw.exe` 的完整路径，不依赖 PATH。
- `--expect-home` 不能用 `--home` 绕过：目录实际映射必须等于期望的 home。
- `home=… source=…` 等提示走 stderr，stdout 只留给 codex，方便管道和外部程序解析。
- codex 参数支持 `--` 分隔，也可不写 `--`（acsw 只认开头的 `--home` / `--expect-home`，其余原样交给 codex）。

### `acsw use <home> <source>` 依次做什么

1. 检查 home 是否空闲：`acsw codex` / `acsw run` 登记过的进程都已退出，否则拒绝。系统里如果还有不是 acsw 启动的 `codex.exe`，只给出提醒；加 `--strict` 时直接拒绝。
2. 同一个 openai source 不能同时给两个 home 用，否则两份 auth.json 会各自刷新 token、互相把对方挤掉。custom source 可以多个 home 同时用。
3. 如果旧 source 是 openai，先把 home 里刷新过的 `auth.json` 存回旧 source。存之前核对 `account_id`，不是同一个账号就拒绝。
4. 放入新 source 的登录。openai 复制 `auth.json`；custom 把 home 里的 `auth.json` 移进备份目录，避免它干扰 custom。
5. 改 `config.toml` 里 acsw 管理的两块：文件开头的 `model_provider`、`model`、`cli_auth_credentials_store`，以及文件末尾的 `[model_providers.custom]`（`wire_api="responses"`、`requires_openai_auth=false`、`supports_websockets=false`、`env_key="ACSW_API_KEY"`）。你写在块外的配置都保留；块外如果有冲突的 `model_provider`、`model`，会被注释掉并加上标记。
6. 如果状态库里有会话记录的 provider 和新 provider 不一样，先把 `state_5.sqlite`（连同 `-wal`、`-shm`）备份，再运行 `UPDATE threads SET model_provider=? WHERE model_provider<>?`。没有表或没有这一列就跳过。旧的自定义名也会一并改成 `custom`。
7. 更新 map.toml。

全程会先备份到 `home\<名称>\.acsw-backup\<时间>\`，并写一份 `manifest.txt`。任何一步失败都会把已经改过的文件全部恢复。`--dry-run` 只列出要做的事，不改任何文件。

### `acsw codex` / `acsw run`

- 按当前目录在 map.toml 里找到 home（或 `--home` / `--expect-home`），在 stderr 打印 `home=… source=… (provider)`，设好 `CODEX_HOME` 再启动 codex。
- 子进程环境里会清掉 `OPENAI_API_KEY`、`CODEX_API_KEY`、`CODEX_SQLITE_HOME`、`OPENAI_BASE_URL`，以及父进程原有的 `CODEX_HOME`。如果是 custom，再从凭据管理器读 key，作为 `ACSW_API_KEY` 只注入这一个子进程。
- 把进程号登记到 `home\<名称>\.acsw-run\`，退出时清理。按 Ctrl+C 由 codex 自己处理。
- 退出码原样返回（含经 `codex.cmd` / `cmd.exe` 中转的情况）。

### `acsw ui`

本机网页，只监听 `127.0.0.1`，可以看状态，也可以点击切换，也能只预演。网页切换和命令行用的是同一套 `internal/core`。防护措施：页面里带随机 token；Host 只认 127.0.0.1 和 localhost；不能被嵌进别的页面。以后做 Wails 托盘界面，也直接调用 `internal/core`。

## 代码结构

```
cmd/acsw            命令行入口
internal/config     根目录、map.toml、source 元数据、路径匹配、文件锁、子进程环境
internal/source     建 source（codex login / --from / custom）
internal/home       use（事务式）、config.toml 管理块、status
internal/codexdb    状态库：备份、批量改 provider、最近会话（先探测列名，缺列时改为扫描 rollout 文件）
internal/proc       运行登记、进程存活检查、codex 进程快照（Windows 用 Toolhelp）
internal/cred       凭据：Windows 用凭据管理器；其他平台用仅供测试的文件实现
internal/core       命令行和网页共用的入口
internal/web        acsw ui
```

## 已知限制

- `threads` 表结构只在 Codex 0.167.0 上核对过，升级 Codex 后要重新确认。`last` 会先探测列名，缺列时改为扫描 `sessions\**\rollout-*.jsonl` 的首行。
- 只认 `state_5.sqlite`，或者 home 根目录下编号最大的 `state_N.sqlite`。如果你的 config 里设了 `sqlite_home`，acsw 找不到这个库。
- 判断“home 空闲”只依据 acsw 的登记（acsw 或中间的 codex.cmd 被强杀后，仍会顺着进程快照的父进程号认出还在跑的 codex.exe）。不经过 acsw 直接用 `CODEX_HOME=home1` 启动的 codex，只能靠 `codex.exe` 进程快照给出提醒，无法确定它属于哪个 home。没有处理 PID 被复用的情况。
- 备份不会自动清理，`.acsw-backup` 里的文件含登录 token，按敏感文件对待。
- map 只按目录前缀匹配项目，不识别 junction 或 subst 盘符的别名路径。

## 自测步骤

先设独立测试根目录，例如 `set ACSW_ROOT=%TEMP%\acsw-test`，全部在测试根目录里做，不碰现有 Codex 主目录和启动脚本。

不调 API 的部分：
1. `acsw source add plus1 --type openai --from <现有 plus 的 CODEX_HOME>`；`acsw source add pro1 --type openai --from %USERPROFILE%\.codex`；`acsw source list` 里应显示“登录：有”。
2. `acsw source add api1 --type custom --base-url https://api.example.com/v1 --model <模型>`，输入 key 时不回显；然后在“凭据管理器 → Windows 凭据”里应能看到 `acsw/codex/api1`。
3. `acsw home add home1`、`acsw home add home2`、`acsw map home1 <项目1>`、`acsw map home2 <项目2>`。
4. `acsw use home1 plus1 --dry-run`，再去掉 `--dry-run` 执行；打开 `home1\config.toml` 检查内容。
5. `acsw use home2 plus1` 应该被拒绝，提示 plus1 正被 home1 使用。
6. 在项目1 下 `acsw codex`，用 Plus 正常对话一轮。退出后运行 `acsw last`，应能看到刚才的会话。**用 sqlite 工具看一下 `home1\state_5.sqlite` 的 `threads` 表结构，确认列名和 acsw 的假设一致。**
7. 运行 codex 期间，在另一个窗口执行 `acsw use home1 api1`，应该被拒绝；`acsw status` 显示 home1 有 1 个在运行。
8. `acsw home cmd home2`，用生成的 `acsw-codex.cmd --version` 在已映射目录应成功；在未映射目录应拒绝、退出码 2。
9. `acsw ui --open`，确认页面能显示状态，也能预演。

要调 API 的部分：
10. 退出 codex，`acsw use home1 api1`。在项目1 下 `acsw codex resume <第 6 步的会话ID>`，应能接上会话并正常回复。确认 home1 里已经没有 `auth.json`，也确认 key 没有出现在任何文件或输出里。
11. 新开一个 api1 会话对话一轮。退出后 `acsw use home1 plus1`，再用 `resume` 接续这个会话，这一轮走 Plus 额度。
12. home1 用 plus1、home2 用 api1，两个同时 `acsw codex`，各对话一轮，确认互不干扰。
13. 回滚检查（不调用）：把 `home1\config.toml` 设为只读，再执行 `acsw use home1 api1`。应提示“已回滚”，`auth.json`、map 和库都应恢复原样。

## 开发

```
go vet ./... && go test ./...
go build -trimpath -o dist/acsw.exe ./cmd/acsw
```

单测：凭据用内存或文件实现，进程检查用假实现，codex 用假的 shell 脚本代替（类 Unix），状态库用 modernc sqlite 造的假库。

## License

MIT
