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

之后在映射到 home1 的目录里运行 `acsw codex`，直接用官方 `.codex` 和它自己的登录，不注入 source 和 key；`acsw last` 只读查会话；对 home1 执行 `acsw use` 会被拒绝（99994008）；acsw 不会往官方 `.codex` 写任何东西。

Afterwards `acsw codex` in directories mapped to home1 uses the official `.codex` and its own login without injecting a source or key; `acsw last` reads sessions read-only; `acsw use` on home1 is refused (99994008); acsw never writes into the official `.codex`.

## Claude Code

```
acsw source add gw1 --tool claude --base-url https://gateway.example.com --model <model> [--sonnet <m>] [--haiku <m>]   # key 存 acsw/claude/gw1
acsw claude gw1                      # 包装启动（默认 helper，key 不落盘）/ wrapper (helper by default, key never on disk)
acsw claude gw1 -p "hi"              # 参数原样交给 claude / arguments go to claude unchanged
acsw claude gw1 --key-mode plain     # 用明文 settings 文件启动 / launch with the plaintext file
acsw claude export gw1               # 生成 helper 文件，给 claude --settings 用 / helper file for claude --settings
acsw claude export gw1 --key-mode plain   # 生成明文文件（有提醒）/ plaintext file (with a warning)
acsw source edit gw1 --model <m> | --set-key
acsw source models gw1               # GET <base_url>/v1/models
acsw source rm gw1 --yes             # 删 key、删生成的文件 / deletes the key and the generated files
```

- 生成的文件在 `%USERPROFILE%\agent-source\claude\settings\`（`<source>.json` 是 helper 文件，`<source>.plain.json` 是明文文件）。直接用：`claude --settings "<文件>"`。
- acsw 不改 `~\.claude`，不设 `CLAUDE_CONFIG_DIR`，会话还在原来的地方。用户级 settings 里有 acsw 盖不住的 `ANTHROPIC_*` 变量时会提醒，`--strict` 时拒绝启动。
- 不要自己再传 `--settings`。source 不能叫 `export` 或 `key`。claude source 不能给 codex home 用。

English:

- Generated files live in `%USERPROFILE%\agent-source\claude\settings\` (`<source>.json` is the helper file, `<source>.plain.json` the plaintext one). Use them directly with `claude --settings "<file>"`.
- acsw does not modify `~\.claude` and does not set `CLAUDE_CONFIG_DIR`; sessions stay where they are. `ANTHROPIC_*` variables in user settings that acsw cannot cover produce a warning; `--strict` refuses to launch.
- Do not pass `--settings` yourself. Sources cannot be named `export` or `key`. A claude source cannot be used by a codex home.

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