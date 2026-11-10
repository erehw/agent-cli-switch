# acsw 简明使用手册 / acsw quick guide

acsw（agent-cli-switch）管理多个 Codex 登录（source），按项目目录分到不同的 home（真正的 CODEX_HOME），每个 home 同一时间只用一个 source；也能给 Claude Code 切换 API 来源（见 Claude Code 一节）。每节先中文、后英文；细节见 README。

acsw (agent-cli-switch) manages several Codex logins (sources) and assigns project directories to homes (real CODEX_HOMEs); each home uses one source at a time. It can also switch Claude Code between API sources (see the Claude Code section). Each section is Chinese first, then English; see README for details.

## 示例设置 / Example setup

- 数据根目录 / data root：`%USERPROFILE%\agent-source\codex`（可用 / override with `ACSW_ROOT`）
- home1：openai source `plus1`（ChatGPT 订阅 / subscription），或官方 `.codex` 的只读别名 / or a read-only alias of the official `.codex`
- home2：custom source `api1`（`https://api.example.com/v1`）
- 程序 / binary：`acsw.exe` 放到 PATH / on PATH

## 日常用法 / Daily use

```
acsw codex                       # 启动 codex，自动用这个目录对应的 home / start codex with this directory's home
acsw last                        # 这个项目最近的会话 ID / recent session IDs for this project
acsw codex resume <id>           # 接上某个会话 / resume a session
acsw codex -m <model>            # 参数原样交给 codex / arguments go to codex unchanged
acsw status [--json]             # 各 home 的 source 和运行数 / sources and running counts; --json for programs
acsw doctor                      # 体检 / health check
acsw ui --open                   # 本机网页 / local web UI
```

- 给别的程序调用（A01）：`acsw home cmd home2` 生成 `home\home2\acsw-codex.cmd`；当前目录必须已映射到 home2，否则拒绝（99994001 或 99994002）。
- 自己在终端（A02）：`acsw codex ...`，没映射也拒绝。不在映射目录时可用 `--home home2`（不能绕过 `--expect-home`）。
- 长 prompt 用 `codex exec -` 从 stdin 传入，acsw 原样转交。

English:

- For other programs (A01): `acsw home cmd home2` generates `home\home2\acsw-codex.cmd`; the current directory must map to home2, otherwise it is refused (99994001 or 99994002).
- In a terminal (A02): `acsw codex ...`, also refused when unmapped. Outside mapped directories use `--home home2` (it cannot bypass `--expect-home`).
- Send long prompts through stdin with `codex exec -`; acsw passes stdin through unchanged.

## 换 source（额度用完时）/ Switching sources (out of quota)

1. 退出这个 home 里所有 codex / quit every codex in the home.
2. 预演 / dry run：`acsw use home2 plus1 --dry-run`
3. 执行 / apply：`acsw use home2 plus1`
4. 照常 `acsw codex`，旧会话可直接 resume / run `acsw codex` as usual; old sessions resume.

规则：同一个 Plus/Pro 登录同时只能给一个 home 用；home 里有 codex 在跑时拒绝切换；不是 acsw 启动的 codex 只提醒（`--strict` 拒绝）；每次切换先备份到 `home\<名称>\.acsw-backup\<时间>\`，出错自动回滚，备份里有 token，别外传。

Rules: one Plus/Pro login serves one home at a time; switching is refused while codex runs in the home; codex not started by acsw only warns (`--strict` refuses); every switch is backed up to `home\<name>\.acsw-backup\<time>\` and rolled back on failure; backups contain tokens, keep them private.

## 管理 / Managing sources, homes and mappings

```
acsw source add plus1 --type openai                          # codex login 登录 / log in
acsw source add pro1  --type openai --from <CODEX_HOME>      # 复制现有登录 / copy an existing login
acsw source add api1  --type custom --base-url https://api.example.com/v1 --model <model>   # key 存凭据管理器 / key to Credential Manager
acsw source list
acsw source edit api1 --base-url <URL> --model <model> --set-key   # 只限 custom / custom only
acsw source rm api1 --yes                     # 只限 custom，在用时拒绝 / custom only, refused while in use
acsw source models api1                       # GET <base_url>/models
acsw home add home3
acsw home rm home3 --yes                      # 目录移到备份区 / directory moved to .acsw-backup
acsw home rm home3 --purge --yes              # 彻底删除（含会话）/ delete for good (sessions too)
acsw home cmd home3                           # 生成启动脚本 / generate the launcher script
acsw map home3 [dir]                          # 不写目录就用当前目录 / default: cwd
acsw unmap [dir]                              # 必须是映射表里的那条目录 / must be an exact mapped directory
```

- `home rm` 要求没有项目映射、没有 codex 在跑（否则 99994009）；先 `acsw unmap`。
- `unmap` 时如果该目录下还有 codex 在跑，它们不受影响，只给提示。

English:

- `home rm` needs no mapped projects and no running codex (otherwise 99994009); run `acsw unmap` first.
- `unmap` is allowed while codex runs in that directory; running processes are unaffected and acsw only warns.

## home1 = 官方 .codex / home1 = the official .codex

```
acsw home alias home1          # 只改 map.toml / only edits map.toml
acsw home alias home1 --off    # 取消 / remove
```

如果 home1 已经是同一个目录的别名，只提示“没有改动”。如果它已经是别的目录的别名、在用 source 或目录里有文件，会先显示现在和改后的状态再问 `[y/N]`（默认 N）；在脚本里（不是终端）会报 `confirm_required`（99994012）而不改，确认无误时加 `--yes`。

If home1 is already an alias of the same directory, acsw just says nothing changed. If it is an alias of another directory, uses a source, or its directory has files, acsw shows the current and new state and asks `[y/N]` (default N); in scripts (no terminal) it fails with `confirm_required` (99994012) and changes nothing; add `--yes` when you are sure.

之后在映射到 home1 的目录里运行 `acsw codex`，直接用官方 `.codex` 和它自己的登录，不注入 source 和 key；`acsw last` 只读查会话；对 home1 执行 `acsw use` 会被拒绝（99994008）；acsw 不会往官方 `.codex` 写任何东西。

Afterwards `acsw codex` in directories mapped to home1 uses the official `.codex` and its own login without injecting a source or key; `acsw last` reads sessions read-only; `acsw use` on home1 is refused (99994008); acsw never writes into the official `.codex`.

## 把官方配置里的 MCP 等设置带到 home / Bring MCP and other settings from the official config into a home

```
acsw home import-base home2 --dry-run   # 先看会改哪些键 / see which keys would change
acsw home import-base home2             # 再导入 / then import
```

只导入和 provider 无关的设置（MCP、features、sandbox、审批、tui、notify、项目信任等）；`model`、`model_provider(s)`、登录和密钥相关的不碰，home 自己的 provider/model 保留。官方 `config.toml` 只读；home 原来的 `config.toml` 先备份到 `.acsw-backup`。别名 home 和正在跑 codex 的 home 会拒绝。网页 Codex 页签里每个 home 的「导入官方基础配置」按钮做同样的事（先预演，确认后写）。

Only provider-independent settings are imported (MCP, features, sandbox, approval, tui, notify, project trust, ...); `model`, `model_provider(s)` and anything about login or keys are left alone, and the home keeps its own provider/model. The official `config.toml` is read-only; the home's old `config.toml` is backed up to `.acsw-backup` first. Alias homes and homes with codex running are refused. The "import official base config" button per home in the web UI's Codex tab does the same (preview first, writes after you confirm).

## Claude Code

```
acsw source add gw1 --tool claude --base-url https://gateway.example.com --model <model> [--sonnet <m>] [--haiku <m>]   # key 存 acsw/claude/gw1
acsw claude gw1                      # 包装启动（默认 helper，key 不落盘）/ wrapper (helper by default, key never on disk)
acsw claude gw1 -p "hi"              # 参数原样交给 claude / arguments go to claude unchanged
acsw claude gw1 --key-mode plain     # 用明文 settings 文件启动 / launch with the plaintext file
acsw claude export gw1               # 生成 helper 文件，给 claude --settings 用 / helper file for claude --settings
acsw claude export gw1 --key-mode plain   # 生成明文文件（有提醒）/ plaintext file (with a warning)
acsw claude cmd gw1                  # 生成 claude-gw1.cmd（放在 acsw.exe 旁边）/ launcher next to acsw.exe
acsw claude cmd gw1 --dir D:\tools --name cgw   # 指定目录和文件名 / explicit directory and name
acsw source edit gw1 --model <m> | --set-key
acsw source models gw1               # GET <base_url>/v1/models
acsw source rm gw1 --yes             # 删 key、删生成的文件 / deletes the key and the generated files
```

- 生成的文件在 `%USERPROFILE%\agent-source\claude\settings\`（`<source>.json` 是 helper 文件，`<source>.plain.json` 是明文文件）。直接用：`claude --settings "<文件>"`。
- `/model` 菜单只显示 source 的模型：生成文件把菜单名字、说明、子代理模型等变量写成空串，用户级 settings（比如 cc-switch 写的别家配置）里的同名变量不会漏进来。升级 acsw 后要重启 claude 才生效（settings 只在启动时读）。
- acsw 不改 `~\.claude`，不设 `CLAUDE_CONFIG_DIR`，会话还在原来的地方。用户级 settings 里有 acsw 盖不住的 `ANTHROPIC_*` 变量时会提醒，`--strict` 时拒绝启动。
- 不要自己再传 `--settings`。source 不能叫 `export`、`key` 或 `cmd`。claude source 不能给 codex home 用。
- `acsw claude cmd gw1` 生成 `claude-gw1.cmd`，内容就是 `"<acsw.exe>" claude gw1 %*` 加 `exit /b %ERRORLEVEL%`：参数原样转交，stdin 继承，退出码传回；文件里不含 key。默认放在 acsw.exe 所在目录（通常在 PATH 里，之后直接 `claude-gw1 -p "hi"`）。acsw 生成的同名文件直接覆盖；别人写的文件不覆盖（99994012 `confirm_required`），加 `--force` 才覆盖。网页 Claude Code 页签每个 source 有「生成 .cmd」按钮。
- 网页（`acsw ui`）分 Codex 和 Claude Code 两个页签；新增/编辑 source 时可点「从上游获取模型」，从列表里选模型填进 model（claude 还有 opus/sonnet/haiku/fable），也可以手填。

English:

- Generated files live in `%USERPROFILE%\agent-source\claude\settings\` (`<source>.json` is the helper file, `<source>.plain.json` the plaintext one). Use them directly with `claude --settings "<file>"`.
- The `/model` menu shows only the source's models: the generated file sets the menu name/description, subagent model and similar variables to empty strings, so same-named variables in user settings (e.g. another provider's config written by cc-switch) cannot leak in. Restart claude after upgrading acsw (settings are read at startup).
- acsw does not modify `~\.claude` and does not set `CLAUDE_CONFIG_DIR`; sessions stay where they are. `ANTHROPIC_*` variables in user settings that acsw cannot cover produce a warning; `--strict` refuses to launch.
- Do not pass `--settings` yourself. Sources cannot be named `export`, `key` or `cmd`. A claude source cannot be used by a codex home.
- `acsw claude cmd gw1` writes `claude-gw1.cmd`, which is just `"<acsw.exe>" claude gw1 %*` plus `exit /b %ERRORLEVEL%`: arguments are passed through, stdin is inherited, the exit code comes back; the file holds no key. By default it goes next to acsw.exe (usually on PATH, so `claude-gw1 -p "hi"` works). An acsw-generated file of the same name is overwritten; a file acsw did not write is left alone (99994012 `confirm_required`) unless you add `--force`. In the web UI's Claude Code tab each source has a "生成 .cmd" (generate .cmd) button.
- The web UI (`acsw ui`) has a Codex tab and a Claude Code tab; when adding or editing a source, "fetch models from upstream" lets you pick models into model (and opus/sonnet/haiku/fable for claude), or type them freely.

## 注意 / Notes

- 导入 Plus/Pro 后，不要再用旧方式启动同一个登录，否则两边刷新 token 会互相挤掉。
- key 只在凭据管理器里（目标名 `acsw/codex/<source>`，claude 的是 `acsw/claude/<source>`），不写进文件；唯一例外是你主动导出的 Claude 明文文件。
- 改代码后运行 `.\build.ps1`（编译 `dist\acsw.exe` 并注入版本号），再复制到 PATH；生成过 `acsw-codex.cmd` 的话，用新 exe 再生成一次。

English:

- After importing a Plus/Pro login, do not keep launching it the old way, or both copies will refresh tokens and log each other out.
- Keys live only in Credential Manager (target `acsw/codex/<source>`, or `acsw/claude/<source>` for Claude), never in files; the only exception is a Claude plaintext file you export on purpose.
- After code changes run `.\build.ps1` (builds `dist\acsw.exe` with the version injected) and copy it to PATH; regenerate any `acsw-codex.cmd` with the new exe.

## 退出码 / Exit codes

codex 或 claude 启动后，acsw 原样返回它们的退出码。acsw 自己出错时，stderr 有一行 `acsw 错误：…`，最后一行是 JSON（`{"acsw_error":…,"kind":…,"msg":…}`）。

Once codex or claude starts, acsw returns its exit code. On its own errors acsw prints `acsw 错误：…` to stderr, and the last line is JSON (`{"acsw_error":…,"kind":…,"msg":…}`).

| 码 Code | kind | 含义 | Meaning |
|---|---|---|---|
| 99994001 | `not_mapped` | 当前目录没映射 | Directory not mapped |
| 99994002 | `expect_home_mismatch` | 映射的 home 不是 `--expect-home` | Mapped home differs from `--expect-home` |
| 99994003 | `no_source` | home 还没指定 source | Home has no source |
| 99994004 | `bad_args` | 用法或参数错误 | Usage error |
| 99994005 | `home_conflict` | `--home` 和 `--expect-home` 冲突 | `--home` vs `--expect-home` conflict |
| 99994006 | `home_not_found` | home 不存在 | Home not found |
| 99994007 | `source_invalid` | source 不存在或类型不对 | Source missing or wrong type |
| 99994008 | `home_readonly_alias` | 官方 .codex 的只读别名，不能写 | Read-only official alias |
| 99994009 | `home_in_use` | 删 home 时还有映射或在跑的 codex | `home rm` while mapped or running |
| 99994010 | `source_tool_mismatch` | source 属于另一个工具（claude / codex） | Source belongs to the other tool |
| 99994011 | `settings_conflict` | `acsw claude --strict` 发现会漏进来的登录设置 | `acsw claude --strict` found leaking login settings |
| 99994012 | `confirm_required` | 会覆盖已有状态，需要确认（`--yes`） | Would overwrite existing state; needs confirmation (`--yes`) |
| 99995001 | `key_unreadable` | 读不到 key | Key unreadable |
| 99995002 | `codex_not_found` | 找不到 codex | codex not found |
| 99995003 | `lock_busy` | 等锁超时 | Lock timeout |
| 99995004 | `codex_start_failed` | codex 启动失败 | codex failed to start |
| 99995005 | `upstream_error` | 上游 `/models` 出错 | Upstream `/models` error |
| 99995006 | `claude_not_found` | 找不到 claude | claude not found |
| 99995007 | `claude_start_failed` | claude 启动失败 | claude failed to start |
| 99995999 | `other` | 其他错误 | Other |

`acsw doctor` 有 error 时返回 1（体检结论，不是 acsw 出错）。/ `acsw doctor` exits 1 when it finds errors (a finding, not an acsw failure).

## License

MIT