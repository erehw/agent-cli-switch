# agent-cli-switch（acsw）

按项目切换 Codex 等 AI 编程 CLI 的登录源：ChatGPT 订阅和 API 一键切换，会话可接续，key 存 Windows 凭据管理器。

Switch the login source of AI coding CLIs such as Codex per project: ChatGPT subscription and API sources in one command, sessions stay resumable, keys live in Windows Credential Manager.

Claude Code 也支持：给 Claude 建 API source（`--tool claude`），用 `acsw claude <source>` 启动，或导出 settings 文件给 `claude --settings` 用；会话位置不变，不改 `~\.claude`。见下文 [Claude Code](#claude-code)。

Claude Code is supported too: create API sources for Claude (`--tool claude`) and launch with `acsw claude <source>`, or export a settings file for `claude --settings`; sessions stay where they are and `~\.claude` is never modified. See [Claude Code](#claude-code) below.

> 文档结构：每节先中文、后英文。改文档时请两种语言一起改。
> Layout: every section has Chinese first, then English. Please update both languages together.

> 已在 Windows 11 + Codex CLI 0.167 上实测：`threads` 表确有 `id`、`model_provider`、`cwd`、`updated_at`（秒）、`title` 列，`cwd` 可能带 `\\?\` 前缀（acsw 已兼容）；`cli_auth_credentials_store = "file"` 和 `supports_websockets = false` 在 `--strict-config` 下都被接受。
> Verified on Windows 11 with Codex CLI 0.167: the `threads` table has `id`, `model_provider`, `cwd`, `updated_at` (seconds) and `title`; `cwd` may carry a `\\?\` prefix (handled); `cli_auth_credentials_store = "file"` and `supports_websockets = false` are accepted under `--strict-config`.

## 安装 / Install

- 直接用：从 GitHub Release 下载 `acsw-<版本>-windows-amd64.zip`，或把自己编译的 `dist/acsw.exe` 放到 PATH 里的目录。
- 自己编译（需要 Go 1.26 或更新版本，不需要 C 编译器）：`.\build.ps1`（输出 `dist\acsw.exe`）或 `.\build.ps1 -Out D:\bin\acsw.exe`。它实际执行 `go build -trimpath -ldflags "-X main.version=<git describe --tags --always --dirty>" -o dist/acsw.exe ./cmd/acsw`。
- 版本号：代码里不写死。没有注入时显示 Go 记录的模块版本，也没有就显示 `dev`。`acsw version` 输出 `acsw <版本>`，开头的 v 统一去掉，例如 `acsw 0.1.1`、`acsw 0.1.1-2-gabc1234-dirty`。
- 数据根目录默认是 `%USERPROFILE%\agent-source\codex`，可以用环境变量 `ACSW_ROOT` 改。测试时请设独立的测试根目录，不要碰正式目录。`acsw doctor`、`acsw status --json`（`root_from`）和网页顶部都会显示当前根目录来自哪里。
- codex 路径：默认用 PATH 里的 `codex`（npm 装的 `codex.cmd` 也可以），也可以用 `ACSW_CODEX` 指定完整路径。
- claude 路径：默认用 PATH 里的 `claude`（npm 装的 `claude.cmd` 也可以），也可以用 `ACSW_CLAUDE` 指定完整路径。只用 codex 的话不需要装 claude。

English:

- Prebuilt: download `acsw-<version>-windows-amd64.zip` from GitHub Releases, or put your own `dist/acsw.exe` in a directory on PATH.
- Build from source (Go 1.26+, no C compiler needed): `.\build.ps1` (writes `dist\acsw.exe`) or `.\build.ps1 -Out D:\bin\acsw.exe`. It runs `go build -trimpath -ldflags "-X main.version=<git describe --tags --always --dirty>" -o dist/acsw.exe ./cmd/acsw`.
- Version: nothing is hard-coded. Without ldflags acsw shows the module version recorded by Go, or `dev`. `acsw version` prints `acsw <version>` with any leading `v` removed, e.g. `acsw 0.1.1` or `acsw 0.1.1-2-gabc1234-dirty`.
- Data root defaults to `%USERPROFILE%\agent-source\codex`; override it with `ACSW_ROOT`. Use a separate root for testing. `acsw doctor`, `acsw status --json` (`root_from`) and the web UI header all show where the root comes from.
- codex binary: `codex` on PATH (the npm `codex.cmd` works), or a full path in `ACSW_CODEX`.
- claude binary: `claude` on PATH (the npm `claude.cmd` works), or a full path in `ACSW_CLAUDE`. Not needed if you only use codex.

## 术语 / Terms

| 名称 Name | 是什么 | What it is | 位置 Location |
|---|---|---|---|
| source | 一份登录或一份 API 配置，比如 plus1、pro1、api1 | One login or one API configuration, e.g. plus1, pro1, api1 | `source\<name>\` |
| provider | 只有两个固定名：`openai`（ChatGPT 订阅）和 `custom`（所有 API source）；官方别名 home 显示 `official` | Two fixed names: `openai` (ChatGPT subscription) and `custom` (all API sources); official alias homes show `official` | home 的 config.toml，以及每条会话 / the home's config.toml and every session |
| home | 真正的 `CODEX_HOME`，存会话、`state_5.sqlite`、共用设置 | The real `CODEX_HOME`: sessions, `state_5.sqlite`, shared settings | `home\<name>\` |
| map.toml | 项目目录前缀 → home；每个 home 当前用哪个 source | Project directory prefix → home; current source of each home | 根目录 / root |

- openai source：`source\plus1\auth.json`，也就是 ChatGPT 登录。custom source：`source\api1\acsw-source.toml` 里存 `base_url` 和 `model`；**key 存在 Windows 凭据管理器**，目标名 `acsw/codex/api1`，不写进任何文件。
- 所有 custom source 的 provider 都叫 `custom`，所以 api1 和 api2 之间切换不用改库；只有 openai 和 custom 之间切换才需要改。
- claude source：`acsw-source.toml` 里多一行 `tool = "claude"`，只能是 custom 类型；key 的目标名是 `acsw/claude/<名称>`。claude source 不能给 codex home 用，codex source 也不能给 `acsw claude` 用（`source_tool_mismatch`）。

English:

- openai source: `source\plus1\auth.json`, i.e. the ChatGPT login. custom source: `base_url` and `model` in `source\api1\acsw-source.toml`; **the key lives in Windows Credential Manager** under target `acsw/codex/api1` and is never written to a file.
- Every custom source uses provider `custom`, so switching between api1 and api2 needs no database change; only switching between openai and custom does.
- claude source: `acsw-source.toml` has an extra `tool = "claude"` line and must be custom; the key target is `acsw/claude/<name>`. A claude source cannot be used by a codex home, and a codex source cannot be used by `acsw claude` (`source_tool_mismatch`).

## 命令 / Commands

```
acsw source add plus1 --type openai                      # 以 CODEX_HOME=source\plus1 运行 codex login / runs codex login
acsw source add pro1  --type openai --from %USERPROFILE%\.codex   # 复制现有 auth.json / copy an existing auth.json
acsw source add api1  --type custom --base-url https://api.example.com/v1 --model gpt-5.x   # 提示输入 key / prompts for the key
acsw source list
acsw source edit api1 --base-url <URL> --model <model> --set-key   # 只限 custom / custom only
acsw source rm api1 --yes                 # 只限 custom；有 home 在用就拒绝 / custom only; refused while in use
acsw source models api1                   # GET <base_url>/models，原样输出 / prints the raw response
acsw home add home1
acsw home rm home3 [--purge] --yes        # 见下文 / see below
acsw home cmd home2                       # 生成 home\home2\acsw-codex.cmd / generate a launcher script
acsw home alias home1 [dir] | --off       # 官方 .codex 只读别名 / read-only alias of the official .codex
acsw map home1 [项目目录 project-dir]      # 默认当前目录；最长前缀优先 / default: cwd; longest prefix wins
acsw unmap [项目目录 project-dir]          # 取消一条映射 / remove one mapping
acsw use home1 plus1 [--dry-run] [--strict]
acsw codex [--home home1] [--expect-home home2] [codex args...]
acsw run [codex] [...]                    # 等同 acsw codex / same as acsw codex
acsw last [--home home1] [-n 5]
acsw status [--json]
acsw doctor [--json]
acsw ui [--port 7799] [--open]
acsw version

# Claude Code（见下文 / see below）
acsw source add gw1 --tool claude --base-url https://gateway.example.com --model <model> [--opus <m>] [--sonnet <m>] [--haiku <m>] [--fable <m>] [--key-mode helper|plain] [--env NAME=value ...]
acsw source edit gw1 [--model <m>] [--opus ""] [--key-mode plain] [--env NAME=value ...|--clear-env] [--set-key]
acsw claude gw1 [--key-mode helper|plain] [--strict] [--] [claude args...]
acsw claude export gw1 [--key-mode helper|plain]
acsw claude key gw1                       # 给 apiKeyHelper 用 / for apiKeyHelper only
```

### 两种启动方式 / Two ways to launch

| 方式 Way | 命令/文件 Command/file | 适合 For | 规则 Rule |
|---|---|---|---|
| A01 | `home\<home>\acsw-codex.cmd`（`acsw home cmd`） | 给别的程序填路径 / other programs that take a path | 当前目录必须已映射到该 home，否则拒绝（99994001/99994002）/ cwd must map to that home, otherwise refused |
| A02 | `acsw codex ...` | 自己在终端敲 / typing in a terminal | 按当前目录找 home；没映射就拒绝（99994001）/ home from cwd; refused if unmapped |

- A01 的脚本等价于 `acsw codex --expect-home <home> %*`，并用 `@exit /b %ERRORLEVEL%` 传出退出码；脚本里写的是生成时 `acsw.exe` 的完整路径。`--expect-home` 不能用 `--home` 绕过。
- 提示信息走 stderr，stdout 只留给 codex。codex 参数可以用 `--` 分隔，也可以不写（acsw 只认开头的 `--home` / `--expect-home`）。
- stdin 原样交给 codex（句柄直接继承），长 prompt 请用 `codex exec -` 走 stdin。

English:

- The A01 script is `acsw codex --expect-home <home> %*` followed by `@exit /b %ERRORLEVEL%`, with the full path of `acsw.exe` at generation time. `--home` cannot override `--expect-home`.
- acsw messages go to stderr; stdout belongs to codex. codex arguments may follow `--` or not (acsw only consumes a leading `--home` / `--expect-home`).
- stdin is passed to codex unchanged (handle inherited); send long prompts via stdin with `codex exec -`.

### 退出码和错误 JSON / Exit codes and the error JSON line

codex 正常启动后，acsw 原样返回 codex 自己的退出码，不额外输出任何东西。下表是 acsw **自己**出错时的退出码（所有子命令适用），定义在 `internal/exitcode`。

Once codex starts, acsw returns codex's own exit code and prints nothing extra. The table lists the codes acsw uses for **its own** errors (all subcommands), defined in `internal/exitcode`.

| Windows 退出码 Exit code | kind | 含义 | Meaning |
|---|---|---|---|
| 99994001 | `not_mapped` | 当前目录没有映射到任何 home（unmap 时：不是映射表里的那条目录） | Directory not mapped (unmap: not an exact mapped directory) |
| 99994002 | `expect_home_mismatch` | 当前目录映射的 home 不是 `--expect-home` | Mapped home differs from `--expect-home` |
| 99994003 | `no_source` | home 还没指定 source | Home has no source |
| 99994004 | `bad_args` | 用法或参数错误 | Usage or argument error |
| 99994005 | `home_conflict` | `--home` 和 `--expect-home` 冲突 | `--home` conflicts with `--expect-home` |
| 99994006 | `home_not_found` | home 不存在或名称不合法 | Home missing or invalid name |
| 99994007 | `source_invalid` | source 不存在、类型不对或元数据损坏 | Source missing, wrong type or broken metadata |
| 99994008 | `home_readonly_alias` | home 是官方 .codex 的只读别名，拒绝写操作 | Home is a read-only official alias; writes refused |
| 99994009 | `home_in_use` | 删除 home 时还有项目映射或在跑的 codex | `home rm` refused: projects mapped or codex running |
| 99994010 | `source_tool_mismatch` | source 属于另一个工具（claude source 用到 codex home，或反过来） | Source belongs to the other tool (claude vs codex) |
| 99994011 | `settings_conflict` | `acsw claude --strict` 发现用户级或项目级 settings 里有盖不住的登录设置 | `acsw claude --strict` found uncovered login settings in user/project settings |
| 99995001 | `key_unreadable` | 凭据管理器里读不到 key | Key not readable from Credential Manager |
| 99995002 | `codex_not_found` | 找不到 codex | codex not found |
| 99995003 | `lock_busy` | 等全局锁超时（10 秒） | Global lock timeout (10 s) |
| 99995004 | `codex_start_failed` | codex 启动失败 | codex failed to start |
| 99995005 | `upstream_error` | 请求上游失败（`/models`） | Upstream request failed (`/models`) |
| 99995006 | `claude_not_found` | 找不到 claude | claude not found |
| 99995007 | `claude_start_failed` | claude 启动失败 | claude failed to start |
| 99995999 | `other` | 其他 acsw 错误 | Any other acsw error |

acsw 自己出错时，stderr 最后两行固定是 / On its own errors, the last two stderr lines are:

```
acsw 错误：当前目录没有映射，期望 home home2。先运行：acsw map home2
{"acsw_error":99994001,"kind":"not_mapped","msg":"当前目录没有映射，期望 home home2。先运行：acsw map home2","dir":"M:\\x","expect_home":"home2"}
```

- claude 正常启动后同样原样返回 claude 的退出码。
- 最后一行是单行 JSON（UTF-8），不含 key，总是输出。固定字段：`acsw_error`（实际退出码）、`kind`（只增不改）、`msg`。可选字段只在有值时出现，按字母排序：`dir`、`home`、`expect_home`、`source`、`url`、`http_status`、`homes`、`projects`、`mapped_dir`、`alias_of` 等。
- 非 Windows 平台退出码只有 0-255：99994xxx 返回 2，99995xxx 返回 1，用 `kind` 区分。
- `acsw doctor` 例外：它是体检结论，没有 error 返回 0，有 error 返回 1，不输出错误 JSON。

English:

- Once claude starts, its exit code is passed through the same way.
- The last line is a single-line JSON (UTF-8), never contains keys, and is always printed. Fixed fields: `acsw_error` (the actual exit code), `kind` (append-only), `msg`. Optional context fields appear only when set, sorted: `dir`, `home`, `expect_home`, `source`, `url`, `http_status`, `homes`, `projects`, `mapped_dir`, `alias_of`, etc.
- On non-Windows platforms exit codes are 0-255: 99994xxx → 2, 99995xxx → 1; use `kind`.
- `acsw doctor` is different: it reports findings, exits 0 without errors and 1 with errors, and prints no error JSON.

### `acsw status --json`

stdout 只输出一个 JSON 对象；出错时 stdout 为空。不含任何 key 或 auth.json 内容。格式版本 `version` 目前是 1：只加字段不升版本，删除或改含义才升。

stdout carries exactly one JSON object; on error stdout is empty. No keys and no auth.json content. Format `version` is 1: fields are only added without a bump; removals or meaning changes bump it.

```json
{
  "version": 1,
  "root": "C:\\Users\\me\\agent-source\\codex",
  "root_from": "default",
  "acsw_version": "0.1.1",
  "codex": { "path": "C:\\Users\\me\\AppData\\Roaming\\npm\\codex.cmd", "from": "PATH", "found": true },
  "homes": [
    {
      "home": "home2", "source": "api1", "provider": "custom",
      "base_url": "https://api.example.com/v1", "model": "gpt-x",
      "has_login": true, "running": 0, "projects": ["M:\\work\\proj"],
      "alias_of": "", "dir": "C:\\Users\\me\\agent-source\\codex\\home\\home2"
    }
  ],
  "sources": [
    { "name": "api1", "type": "custom", "base_url": "https://api.example.com/v1", "model": "gpt-x", "has_login": true, "used_by": ["home2"] }
  ]
}
```

| 字段 Field | 说明 | Description |
|---|---|---|
| `root` / `root_from` | 数据根目录；`env`（ACSW_ROOT）或 `default` | Data root; `env` (ACSW_ROOT) or `default` |
| `acsw_version` | acsw 程序版本 | acsw build version |
| `codex` | `path`、`from`（`ACSW_CODEX`/`PATH`）、`found`、`error` | `path`, `from` (`ACSW_CODEX`/`PATH`), `found`, `error` |
| `homes[].home` | home 名，按名称排序 | Home name, sorted |
| `homes[].dir` | codex 实际用的 CODEX_HOME，和 `acsw codex` 设置的一样；会话在 `<dir>\sessions`。**不要用 root 拼路径** | The CODEX_HOME codex really uses (same as `acsw codex`); sessions are in `<dir>\sessions`. **Never derive it from root** |
| `homes[].source` / `provider` | 当前 source；`openai`/`custom`/`official`；未设置为 `""` | Current source; `openai`/`custom`/`official`; `""` if unset |
| `homes[].base_url` / `model` | 仅 custom | custom only |
| `homes[].has_login` | custom：有 key；openai：有 auth.json；别名：官方目录有 auth.json；未设置为 `null` | custom: key present; openai: auth.json present; alias: official auth.json exists; `null` if unset |
| `homes[].running` | acsw 登记的、仍在运行的 codex 个数 | Running codex processes registered by acsw |
| `homes[].projects` | 映射到这个 home 的项目目录，总是数组 | Mapped project directories, always an array |
| `homes[].problem` | 有问题时才出现 | Present only when something is wrong |
| `homes[].alias_of` | 总会出现：官方别名 home 为官方目录，普通 home 为 `""` | Always present: official dir for alias homes, `""` otherwise |
| `sources[]` | `name`、`type`、`base_url`/`model`、`has_login`、`used_by` | `name`, `type`, `base_url`/`model`, `has_login`, `used_by` |

`claude` 对象（后加的，`version` 仍是 1）：`claude`（acsw 会用的 claude：`path`、`from`、`found`）、`settings_dir`、`config_dir`（`CLAUDE_CONFIG_DIR` 或 `~\.claude`）、`sessions_dir`（`<config_dir>\projects`）、`sources`（每个 claude source 的 `name`、`base_url`、`models`（`model`/`opus`/`sonnet`/`haiku`/`fable`）、`key_mode`、`has_key`、`extra_env`（只有名字）、`settings`（helper 文件的 `path`、`exists`、`current`）、可选的 `plain`、`launch`）。顶层 `sources` 只列 codex 的 source，claude source 只出现在 `claude.sources` 里。

The `claude` object (added later; `version` stays 1): `claude` (the claude binary acsw would use: `path`, `from`, `found`), `settings_dir`, `config_dir` (`CLAUDE_CONFIG_DIR` or `~\.claude`), `sessions_dir` (`<config_dir>\projects`), and `sources` (per claude source: `name`, `base_url`, `models` (`model`/`opus`/`sonnet`/`haiku`/`fable`), `key_mode`, `has_key`, `extra_env` (names only), `settings` (helper file `path`, `exists`, `current`), optional `plain`, `launch`). The top-level `sources` array lists codex sources only; claude sources appear only under `claude.sources`.

### `acsw doctor`

检查数据根目录（和来源）、配置锁、codex 路径、map.toml、每个 source 的 key 或登录（只看有没有，不读内容）、每个 home（目录、source、官方别名目录和 auth.json 是否存在）、项目目录是否存在、运行登记（清理已退出进程的登记）。每项是 `ok` / `warn` / `error`；`--json` 输出 `{"version":1,"ok":…,"env":{…},"checks":[{"name","level","msg"}]}`。有 error 时退出码 1。

Checks the data root (and where it comes from), the config lock, the codex path, map.toml, every source's key or login (presence only, never content), every home (directory, source, official alias directory and its auth.json), mapped project directories, and run records (stale ones are cleaned). Each check is `ok` / `warn` / `error`; `--json` prints `{"version":1,"ok":…,"env":{…},"checks":[{"name","level","msg"}]}`. Exit code 1 if any error.

有 claude source 时另查：claude 能否找到和版本（和实测过的 2.1.x 不同时提醒）、每个 claude source 的 key、生成的 settings 文件是否完整、apiKeyHelper 路径是否加了引号且程序存在、文件是否和当前设置一致、是否有明文文件、用户级 `settings.json` 里有哪些 `ANTHROPIC_*`（只看名字，不读值，不改文件）。

With claude sources it also checks: claude found and its version (warns when it differs from the tested 2.1.x), each claude source's key, generated settings file completeness, the apiKeyHelper path quoted and its program present, the file matching the current settings, plaintext files present, and which `ANTHROPIC_*` names the user `settings.json` has (names only, never values; the file is not modified).

### `acsw unmap` / `acsw home rm`

- `acsw unmap [目录]`：删除一条映射，目录必须正好是映射表里的那条（子目录会被拒绝，并提示它属于哪条上级映射）。**在跑的 codex 不受影响**（它启动时已拿到 CODEX_HOME），所以允许取消，只提示还有几个在跑；之后在该目录运行 `acsw codex` 会报未映射。
- `acsw home rm <名称> --yes`：要求没有项目映射到它、没有 acsw 登记的 codex 在跑（否则 99994009）。默认把 acsw 根目录下的 home 目录（含 codex 会话）整个**移到备份区** `.acsw-backup\<时间>-home-<名称>`，可以找回；`--purge` 才彻底删除。官方别名 home 只删 acsw 自己的 home 目录和 map 记录，**官方目录不动**。网页上的删除只做移到备份区。

English:

- `acsw unmap [dir]` removes one mapping; the directory must be exactly a mapped entry (a subdirectory is refused with a hint naming the parent mapping). **Running codex processes are unaffected** (they already have their CODEX_HOME), so unmapping is allowed and only reports how many are still running; afterwards `acsw codex` in that directory reports "not mapped".
- `acsw home rm <name> --yes` requires no mapped projects and no running codex registered by acsw (otherwise 99994009). By default the home directory under the acsw root (including codex sessions) is **moved to** `.acsw-backup\<time>-home-<name>` and can be restored; `--purge` deletes it. For an official alias home only acsw's own home directory and map entry are removed; **the official directory is never touched**. The web UI only offers the move-to-backup removal.

### 官方 .codex 别名 / Official .codex alias (home alias)

```
acsw home alias home1                      # 默认 / default %USERPROFILE%\.codex
acsw home alias home1 D:\other\.codex      # 指定目录 / explicit directory
acsw home alias home1 --off                # 取消 / remove
```

- 只改 `map.toml`（`[home.home1]` 下的 `alias_of`），不建 junction，不碰官方目录。home 已在用 source、目录不存在、目录和 acsw 根目录互相包含、同一个官方目录已有别名、home 里有 codex 在跑时，拒绝设置。
- `acsw codex`：`CODEX_HOME` 设为官方目录，不注入 source 和 key，codex 用官方目录里自己的登录；照样清掉 `OPENAI_API_KEY`、`ACSW_API_KEY` 等变量。
- `acsw last` 和网页的“最近会话”：把状态库连同 `-wal` 复制到临时目录再查，失败时扫描 `sessions`；官方目录里不会产生任何文件。
- acsw 从不写官方目录：`use`（命令行和网页）都会被拒绝（99994008）。运行登记放在 acsw 自己的 `home\<home>\.acsw-run`。

English:

- Only `map.toml` changes (`alias_of` under `[home.home1]`); no junction, the official directory is not touched. Refused when the home already uses a source, the directory is missing, it overlaps the acsw root, another home already aliases it, or codex is running in the home.
- `acsw codex` sets `CODEX_HOME` to the official directory, injects no source or key, and codex uses its own login there; `OPENAI_API_KEY`, `ACSW_API_KEY` etc. are still removed from the environment.
- `acsw last` and the web "recent sessions" copy the state database (with `-wal`) to a temp directory and query the copy, falling back to scanning `sessions`; nothing is created in the official directory.
- acsw never writes the official directory: `use` (CLI and web) is refused (99994008). Run records live in acsw's own `home\<home>\.acsw-run`.

### `acsw use <home> <source>` 依次做什么 / What `use` does

1. 检查 home 空闲：acsw 登记的进程都已退出，否则拒绝；不是 acsw 启动的 `codex.exe` 只提醒，`--strict` 时拒绝。
2. 同一个 openai source 不能同时给两个 home 用（两份 auth.json 会互相挤掉 token）；custom source 可以。
3. 旧 source 是 openai 时，先把 home 里刷新过的 `auth.json` 存回旧 source（核对 `account_id`）。
4. 放入新 source 的登录：openai 复制 `auth.json`；custom 把 home 里的 `auth.json` 移进备份。
5. 改 `config.toml` 里 acsw 管理的两块（开头的 `model_provider`/`model`/`cli_auth_credentials_store`，末尾的 `[model_providers.custom]`）；块外配置保留，冲突的键会被注释掉。
6. 会话的 provider 和新 provider 不同时，先备份 `state_5.sqlite`（连同 `-wal`、`-shm`），再批量改 `threads.model_provider`。
7. 更新 map.toml。

全程先备份到 `home\<名称>\.acsw-backup\<时间>\`（含 `manifest.txt`），任何一步失败都会恢复。`--dry-run` 只列出步骤。

English:

1. The home must be idle: every process registered by acsw has exited; foreign `codex.exe` processes only warn unless `--strict`.
2. One openai source cannot serve two homes at once (their auth.json files would fight over tokens); custom sources can.
3. If the old source is openai, the refreshed `auth.json` is saved back to it first (`account_id` is checked).
4. The new login is installed: openai copies `auth.json`; custom moves the home's `auth.json` into the backup.
5. The two acsw-managed blocks in `config.toml` are rewritten (top-level `model_provider`/`model`/`cli_auth_credentials_store`, and `[model_providers.custom]` at the end); your other settings stay, conflicting keys are commented out.
6. If sessions use a different provider, `state_5.sqlite` (with `-wal`, `-shm`) is backed up and `threads.model_provider` is updated.
7. map.toml is updated.

Everything is backed up to `home\<name>\.acsw-backup\<time>\` (with `manifest.txt`) and restored if any step fails. `--dry-run` only lists the steps.

### `acsw codex` / `acsw run`

- 按当前目录找 home（或 `--home` / `--expect-home`），stderr 打印 `home=… source=…`，设好 `CODEX_HOME` 后启动 codex。
- 子进程环境清掉 `OPENAI_API_KEY`、`CODEX_API_KEY`、`CODEX_SQLITE_HOME`、`OPENAI_BASE_URL` 和原有的 `CODEX_HOME`；custom 时从凭据管理器读 key，作为 `ACSW_API_KEY` 只注入这个子进程。
- 进程登记在 `home\<名称>\.acsw-run\`，退出时清理；Ctrl+C 由 codex 处理；退出码原样返回。

English:

- Finds the home from the current directory (or `--home` / `--expect-home`), prints `home=… source=…` to stderr, sets `CODEX_HOME` and starts codex.
- The child environment drops `OPENAI_API_KEY`, `CODEX_API_KEY`, `CODEX_SQLITE_HOME`, `OPENAI_BASE_URL` and any inherited `CODEX_HOME`; for custom sources the key is read from Credential Manager and injected as `ACSW_API_KEY` into that child only.
- The process is registered in `home\<name>\.acsw-run\` and cleaned up on exit; Ctrl+C goes to codex; the exit code is passed through.

### Claude Code

三种用法，都基于 `claude --settings <文件>`。acsw 生成的文件每次都把整套变量写全，不用的写空串：`ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`（helper 模式为空）、`ANTHROPIC_API_KEY=""`、`ANTHROPIC_MODEL` 和 `ANTHROPIC_DEFAULT_OPUS/SONNET/HAIKU/FABLE_MODEL`、`ANTHROPIC_SMALL_FAST_MODEL=""`、`ANTHROPIC_CUSTOM_HEADERS=""`、`CLAUDE_CODE_USE_BEDROCK/VERTEX/FOUNDRY=""`，再加 source 的附加变量。这样用户级、项目级 settings 里的同名变量都会被盖住（沙盒实测：`--settings` 里写了的变量优先，没写的会从别处漏进来）。acsw 不设 `CLAUDE_CONFIG_DIR`，会话还在 `~\.claude`，也不改 `~\.claude` 里的任何文件。

| 用法 | 命令 | key 在哪 |
|---|---|---|
| 1 明文文件 | `acsw claude export gw1 --key-mode plain`，再 `claude --settings <输出的路径>` | 明文写在 `<settings 目录>\gw1.plain.json`（stderr 会提醒；同目录生成 `.gitignore`） |
| 2 包装启动 | `acsw claude gw1 [claude 参数]` | 默认 helper 模式，不落盘；`--key-mode plain` 时用明文文件 |
| 3 helper 文件 | `acsw claude export gw1`，再 `claude --settings <输出的路径>` | 文件里只有 `apiKeyHelper`：`"<acsw.exe 完整路径>" claude key gw1`，claude 需要 key 时调用 acsw 从凭据管理器取 |

- settings 目录：`<数据根目录的上一级>\claude\settings`，默认 `%USERPROFILE%\agent-source\claude\settings`。helper 文件 `<source>.json` 每次启动前按当前设置重新生成（内容没变不写）；`source edit` 后会重写 helper 文件、删掉过时的明文文件。
- `acsw claude <source>`：stdin、stdout、stderr 和退出码原样透传，提示信息走 stderr；子进程环境里去掉 `ANTHROPIC_*` 和 `CLAUDE_CODE_USE_BEDROCK/VERTEX/FOUNDRY`（保留 `CLAUDE_CONFIG_DIR`）；不能再传 `--settings`（`bad_args`）。开头的 `--key-mode`、`--strict` 归 acsw，从第一个别的参数或 `--` 起全部交给 claude。
- 启动前检查用户级 `settings.json` 和当前目录的 `.claude\settings.json`、`.claude\settings.local.json`：有 acsw 盖不住的 `ANTHROPIC_*` 变量（plain 模式下还有 `apiKeyHelper`）时在 stderr 提醒（只写变量名）；加 `--strict` 时拒绝启动（`settings_conflict`）。
- apiKeyHelper 的路径必须带引号（实测不带引号会静默失败、发出空认证），acsw 总是加引号；路径含 `%`、`^`、`"`、`!` 时拒绝生成。helper 模式下文件里 `ANTHROPIC_AUTH_TOKEN` 写空串，否则用户级的 token 也会被发出去。
- `acsw claude key <source>` 把 key 打到 stdout，只给 apiKeyHelper 用。
- source 名不能叫 `export` 或 `key`。附加变量（`--env`）名字含 `KEY`、`TOKEN`、`SECRET` 的拒绝，也不能改 acsw 管理的变量。
- `acsw source models <claude source>` 请求 `<base_url>/v1/models`，同时带 `x-api-key`、`Authorization: Bearer` 和 `anthropic-version`。
- 已在 claude 2.1.319 上用本地假服务实测（见 `cmd/acsw/claude_e2e_test.go`）：三种用法都只把对应 source 的 key 发到对应地址；helper 模式下 key 同时出现在 `x-api-key` 和 `Authorization`，plain 模式只在 `Authorization`；用户级 settings 里的 token 不会漏出去。

English:

Three ways, all built on `claude --settings <file>`. Every generated file sets the full set of variables, empty when unused: `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN` (empty in helper mode), `ANTHROPIC_API_KEY=""`, `ANTHROPIC_MODEL` and `ANTHROPIC_DEFAULT_OPUS/SONNET/HAIKU/FABLE_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL=""`, `ANTHROPIC_CUSTOM_HEADERS=""`, `CLAUDE_CODE_USE_BEDROCK/VERTEX/FOUNDRY=""`, plus the source's extra variables. That overrides the same names in user and project settings (sandbox finding: variables set in `--settings` win, unset ones leak in from elsewhere). acsw never sets `CLAUDE_CONFIG_DIR`, so sessions stay in `~\.claude`, and nothing under `~\.claude` is modified.

| Way | Command | Where the key is |
|---|---|---|
| 1 plaintext file | `acsw claude export gw1 --key-mode plain`, then `claude --settings <printed path>` | Plaintext in `<settings dir>\gw1.plain.json` (stderr warning; a `.gitignore` is created next to it) |
| 2 wrapper | `acsw claude gw1 [claude args]` | helper mode by default, nothing on disk; `--key-mode plain` uses the plaintext file |
| 3 helper file | `acsw claude export gw1`, then `claude --settings <printed path>` | The file only has `apiKeyHelper`: `"<full path of acsw.exe>" claude key gw1`; claude calls acsw to read the key from Credential Manager |

- Settings directory: `<parent of the data root>\claude\settings`, by default `%USERPROFILE%\agent-source\claude\settings`. The helper file `<source>.json` is regenerated before every launch (only written when it changed); `source edit` rewrites the helper file and deletes a stale plaintext file.
- `acsw claude <source>`: stdin, stdout, stderr and the exit code are passed through, acsw messages go to stderr; `ANTHROPIC_*` and `CLAUDE_CODE_USE_BEDROCK/VERTEX/FOUNDRY` are removed from the child environment (`CLAUDE_CONFIG_DIR` is kept); passing `--settings` yourself is `bad_args`. Leading `--key-mode` and `--strict` belong to acsw; everything from the first other argument or `--` goes to claude.
- Before launching, the user `settings.json` and the current directory's `.claude\settings.json` / `.claude\settings.local.json` are checked: `ANTHROPIC_*` variables acsw does not cover (and, in plain mode, `apiKeyHelper`) produce a stderr warning with the names only; with `--strict` the launch is refused (`settings_conflict`).
- The apiKeyHelper path must be quoted (unquoted, claude silently fails and sends empty auth), so acsw always quotes it and refuses paths containing `%`, `^`, `"` or `!`. In helper mode `ANTHROPIC_AUTH_TOKEN` is written as an empty string; otherwise the user-level token would be sent too.
- `acsw claude key <source>` prints the key to stdout; it is meant for apiKeyHelper only.
- Source names `export` and `key` are reserved. Extra variables (`--env`) whose names contain `KEY`, `TOKEN` or `SECRET` are refused, as are acsw-managed names.
- `acsw source models <claude source>` calls `<base_url>/v1/models` with `x-api-key`, `Authorization: Bearer` and `anthropic-version`.
- Verified on claude 2.1.319 against local fake servers (see `cmd/acsw/claude_e2e_test.go`): all three ways send only the source's key to the source's address; in helper mode the key is in both `x-api-key` and `Authorization`, in plain mode only in `Authorization`; the token from the user settings never leaks.

### `acsw ui`

本机网页，和命令行用同一套 `internal/core`。能做的事：顶部显示数据根目录（及来源）、版本、codex 路径；新增/删除 home（删除只移到备份区）；设置或取消官方别名；生成 `.cmd`；映射和取消映射项目目录（映射要求完整路径且目录存在）；切换 source（可预演）；custom source 的新增/编辑/删除/查模型；复制启动命令（cmd / PowerShell）；每个 home 的最近会话；体检；Claude Code 部分：claude source 的新增/编辑/删除/查模型、复制 `acsw claude <source>`、导出 helper 或明文 settings 文件（明文要确认）。openai 登录、`--purge` 只能在命令行做。

安全：只监听 `127.0.0.1`；Host 只认 127.0.0.1 和 localhost（防 DNS 重绑定）；带 `Origin` 头的请求必须同源；所有 `/api/` 请求（包括 GET）都要页面里嵌的随机 token（`X-ACSW-Token`）；POST 只收 JSON 且限制大小；不能被嵌进别的页面；服务端设置了读、写、空闲超时。

English:

A local web page that uses the same `internal/core` as the CLI. It shows the data root (and its source), version and codex path; adds/removes homes (removal only moves to the backup area); sets or clears the official alias; generates `.cmd` scripts; maps and unmaps project directories (mapping needs an absolute, existing directory); switches sources (with dry run); adds/edits/deletes custom sources and queries their models; copies launch commands (cmd / PowerShell); lists recent sessions per home; runs doctor; and in the Claude Code section adds/edits/deletes claude sources, queries their models, copies `acsw claude <source>`, and exports helper or plaintext settings files (plaintext needs confirmation). openai login and `--purge` are CLI-only.

Security: listens on `127.0.0.1` only; Host must be 127.0.0.1 or localhost (DNS rebinding); requests with an `Origin` header must be same-origin; every `/api/` request, GET included, needs the random token embedded in the page (`X-ACSW-Token`); POST bodies must be JSON and size-limited; framing is denied; read/write/idle timeouts are set.

## 代码结构 / Code layout

```
cmd/acsw            命令行入口 / CLI entry
internal/config     根目录、map.toml、source 元数据、路径匹配、文件锁、子进程环境 / root, map.toml, source metadata, paths, lock, child env
internal/source     建 source / create sources (codex login, --from, custom, claude)
internal/claude     生成 Claude settings 文件、检查别处的 settings / build Claude settings files, scan other settings
internal/home       use（事务式）、config.toml 管理块、status / transactional use, managed config blocks, status
internal/codexdb    状态库：备份、改 provider、最近会话 / state DB: backup, provider rewrite, recent sessions
internal/proc       运行登记、进程检查 / run registry, process checks
internal/cred       凭据：Windows 凭据管理器；其他平台为仅供测试的文件实现 / Credential Manager on Windows, test-only file store elsewhere
internal/exitcode   退出码和 kind / exit codes and kinds
internal/core       命令行和网页共用的入口 / shared entry for CLI and web
internal/web        acsw ui
```

## 已知限制 / Known limitations

- `threads` 表结构只在 Codex 0.167.0 上核对过；`last` 会先探测列名，缺列时扫描 `sessions\**\rollout-*.jsonl`。
- 只认 `state_5.sqlite` 或编号最大的 `state_N.sqlite`；设了 `sqlite_home` 时找不到库。
- 判断 home 空闲只依据 acsw 的登记和 `codex.exe` 进程快照；不经过 acsw 启动的 codex 只能提醒；没有处理 PID 复用。
- 备份不会自动清理，`.acsw-backup` 里有登录 token 和会话，按敏感文件对待。
- map 只按目录前缀匹配，不识别 junction 或 subst 盘符的别名路径。
- Claude：`--settings` 和 apiKeyHelper 的行为只在 claude 2.1.319 上实测过；项目级 settings 只检查当前目录下的 `.claude`，不往上找；企业托管的 managed settings 不检查；plain 模式下别处的 `apiKeyHelper` 是否会被调用没有实测，所以只提醒。导出的 helper 文件里 acsw 路径是导出时的位置，挪了 acsw.exe 要重新导出（doctor 会提醒）；它也不记数据根目录，`ACSW_ROOT` 不同时取不到 key。

English:

- The `threads` schema was checked on Codex 0.167.0 only; `last` probes columns and falls back to scanning `sessions\**\rollout-*.jsonl`.
- Only `state_5.sqlite` or the highest `state_N.sqlite` is used; a custom `sqlite_home` is not found.
- Idle detection relies on acsw's registry and a `codex.exe` process snapshot; codex started outside acsw only warns; PID reuse is not handled.
- Backups are never cleaned automatically; `.acsw-backup` holds login tokens and sessions, treat it as sensitive.
- Mapping is by directory prefix only; junction or subst aliases are not recognised.
- Claude: the `--settings` and apiKeyHelper behaviour was verified on claude 2.1.319 only; project settings are checked in the current directory's `.claude` only, not parent directories; enterprise managed settings are not checked; whether another `apiKeyHelper` still runs in plain mode was not tested, so it only warns. An exported helper file contains the acsw path at export time; re-export after moving acsw.exe (doctor warns). It does not record the data root either, so a different `ACSW_ROOT` cannot find the key.

## 自测步骤 / Manual test plan

先设独立测试根目录，例如 `set ACSW_ROOT=%TEMP%\acsw-test`，全部在测试根目录里做。/ First set a separate root, e.g. `set ACSW_ROOT=%TEMP%\acsw-test`, and do everything there.

不调 API / Without API calls:
1. `acsw source add plus1 --type openai --from <plus CODEX_HOME>`；`acsw source add pro1 --type openai --from %USERPROFILE%\.codex`；`acsw source list` 显示登录：有 / shows a login.
2. `acsw source add api1 --type custom --base-url https://api.example.com/v1 --model <model>`；凭据管理器里有 / Credential Manager has `acsw/codex/api1`.
3. `acsw home add home1`、`acsw home add home2`、`acsw map home1 <prj1>`、`acsw map home2 <prj2>`。
4. `acsw use home1 plus1 --dry-run`，再正式执行，检查 / then for real, inspect `home1\config.toml`.
5. `acsw use home2 plus1` 应被拒绝 / must be refused (plus1 is used by home1).
6. 在 prj1 下 `acsw codex` 对话一轮，退出后 `acsw last` 能看到会话 / chat once in prj1, then `acsw last` lists it.
7. codex 运行期间 `acsw use home1 api1` 应被拒绝，`acsw status` 显示 1 个在运行 / while codex runs, `use` is refused and `status` shows 1 running.
8. `acsw home cmd home2`；生成的 `acsw-codex.cmd --version` 在已映射目录成功、在未映射目录退出码 99994001 / succeeds in a mapped dir, exits 99994001 elsewhere.
9. `acsw doctor`；`acsw ui --open`，页面能显示根目录、状态，能预演 / page shows root and status, dry run works.

调 API / With API calls:
10. 退出 codex，`acsw use home1 api1`，`acsw codex resume <第 6 步的会话>` 能接上；home1 里没有 `auth.json`，key 不出现在任何文件或输出 / resume works, no `auth.json` in home1, key never appears.
11. 新开 api1 会话，退出后 `acsw use home1 plus1` 再 resume / new api1 session, switch back to plus1, resume.
12. home1 用 plus1、home2 用 api1，同时运行互不干扰 / both homes at once without interference.
13. 回滚检查：把 `home1\config.toml` 设为只读，`acsw use home1 api1` 应提示已回滚 / make config.toml read-only; `use` must roll back.

## 开发 / Development

```
go vet ./... && go test ./...
.\build.ps1
```

- 单测：凭据用内存或文件实现，进程检查用假实现，codex 用测试二进制或 shell 脚本冒充，状态库用 modernc sqlite 造的假库。所有测试都在临时根目录里跑。
- claude 端到端测试 `TestClaudeE2E`：编译 acsw 到临时目录，`ACSW_ROOT`、`CLAUDE_CONFIG_DIR` 都用临时目录，起本地假服务，用假 key 跑真的 claude；凭据目标名带随机后缀，结束时删除。找不到 claude 或 go、`-short`、`ACSW_SKIP_CLAUDE_E2E=1` 时跳过。
- CI（`.github/workflows/ci.yml`）：Windows 和 Ubuntu 上跑 vet 和 test，Ubuntu 上另查 gofmt。发布（`release.yml`）：推送 `v*` 标签时测试、编译 Windows amd64 版 `acsw.exe`（版本号取标签名）并上传到 GitHub Release。
- 提交说明和 README 一样用中英双语。

English:

- Tests use in-memory or file credentials, fake process checks, the test binary or a shell script as a fake codex, and a fake state DB built with modernc sqlite. All tests run under a temporary root.
- The claude end-to-end test `TestClaudeE2E` builds acsw into a temp dir, uses temp dirs for `ACSW_ROOT` and `CLAUDE_CONFIG_DIR`, starts local fake servers and runs the real claude with dummy keys; credential targets carry a random suffix and are deleted at the end. Skipped when claude or go is missing, with `-short`, or with `ACSW_SKIP_CLAUDE_E2E=1`.
- CI (`.github/workflows/ci.yml`) runs vet and tests on Windows and Ubuntu, plus gofmt on Ubuntu. Release (`release.yml`): on a `v*` tag it tests, builds `acsw.exe` for Windows amd64 (version = tag) and uploads it to a GitHub Release.
- Commit messages are bilingual, like this README.

## License

MIT