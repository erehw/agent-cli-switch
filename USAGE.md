# acsw 简明使用手册

acsw（agent-cli-switch）管理多个 Codex 登录（source），按项目目录分到不同的 home（真正的 CODEX_HOME），每个 home 同一时间只用一个 source。

## 示例设置

- 根目录：`%USERPROFILE%\agent-source\codex`
- home1：openai source `plus1`（ChatGPT 订阅）
- home2：custom source `api1`（`https://api.example.com/v1`）
- 程序：把 `dist/acsw.exe` 放到 PATH

## 日常用法

在已映射的项目目录里：

```
acsw codex                       # 启动 codex（交互界面），自动用这个目录对应的 home
acsw last                        # 看这个项目最近的会话 ID
acsw codex resume <会话ID>       # 接上某个会话
acsw codex -m <模型>             # 参数原样交给 codex（也可写成 acsw codex -- -m <模型>）
acsw status                      # 各 home 用哪个 source、有几个在运行
acsw status --json               # 同上，JSON 格式，给程序读（字段见 README）；homes[].dir 是 codex 实际用的 CODEX_HOME
acsw ui --open                   # 本机网页，看状态、点击切换
```

给别的程序调用（A01）：

```
acsw home cmd home2              # 生成 home\home2\acsw-codex.cmd
...\home\home2\acsw-codex.cmd [codex 参数...]
```

当前目录必须已映射到 home2，否则拒绝（退出码 99994001 或 99994002）。自己在终端用 A02：`acsw codex ...`，按 map 自动找 home，没映射也拒绝。

不在已映射的目录里时，用 `--home home2` 指定（A01 的 `--expect-home` 不能靠这个绕过）。

## 换 source（额度用完时）

1. 先退出这个 home 里所有 codex。
2. 先预演：`acsw use home2 plus1 --dry-run`
3. 执行：`acsw use home2 plus1`
4. 照常 `acsw codex`，旧会话可以直接 resume。

规则：
- 同一个 Plus/Pro 登录同时只能给一个 home 用，acsw 会拦。API source 可以多个 home 同时用。
- home 里有 codex 在跑时 acsw 会拒绝切换。
- 不是 acsw 启动的 codex（旧启动脚本、桌面版）只会提醒，加 `--strict` 则直接拒绝。
- 每次切换都先备份到 `home\<名称>\.acsw-backup\<时间>\`，出错自动回滚。备份里有登录 token，别外传。

## 加东西

```
acsw source add plus1 --type openai                       # 运行 codex login 登录
acsw source add pro1  --type openai --from <现有CODEX_HOME>  # 复制现有登录
acsw source add api1  --type custom --base-url https://api.example.com/v1 --model <模型>   # 提示输入 key，存入凭据管理器
acsw source list
acsw source edit api1 --base-url <URL> --model <模型> --set-key   # 只改 custom；不写的项不改，在用的 home 自动重新应用
acsw source rm api1 --yes                  # 只删 custom；有 home 在用就拒绝；key 删除、目录移到 .acsw-backup
acsw source models api1                    # 只限 custom：用凭据管理器里的 key 请求 <base_url>/models，stdout 原样输出响应体
acsw home add home3
acsw map home3 [项目目录]                                  # 不写目录就用当前目录；子目录也算，最长前缀优先
acsw home cmd home3                                        # 生成给外部程序用的启动脚本
```

## 注意

- 导入 Plus/Pro 后，不要再用旧方式启动同一个登录，否则两边刷新 token 会互相挤掉。
- key 只在凭据管理器里（目标名 `acsw/codex/<source>`），不写进文件。
- 改代码后重新 `go build -trimpath -o dist/acsw.exe ./cmd/acsw`，再复制到 PATH 目录；若用 `acsw home cmd` 生成过脚本，用新 exe 再生成一次，脚本里的路径才会更新。

## home1 = 官方 .codex

```
acsw home alias home1        # 只改 map.toml，把 home1 设为 %USERPROFILE%\.codex 的只读别名
```

之后在映射到 home1 的目录里运行 `acsw codex`，会直接用官方 `.codex` 和它自己的登录启动 codex，不注入 source 和 key；`acsw last` 也从官方目录只读查会话。对 home1 执行 `acsw use` 会被拒绝（99994008），acsw 不会往官方 `.codex` 写任何东西。取消别名：`acsw home alias home1 --off`。
## License

MIT
## 退出码

codex 启动后，acsw 原样返回 codex 自己的退出码。acsw 自己出错时，stderr 有一行 `acsw 错误：…`，退出码如下（Windows）：

| 码 | 含义 |
|---|---|
| 99994001 | 当前目录没映射 |
| 99994002 | 当前目录映射的 home 不是 `--expect-home` 指定的 |
| 99994003 | home 还没指定 source |
| 99994004 | 用法或参数错误 |
| 99994005 | `--home` 和 `--expect-home` 冲突 |
| 99994006 | home 不存在 |
| 99994007 | source 不存在或类型不对 |
| 99994008 | home 是官方 .codex 的只读别名，不能 use |
| 99995001 | 读不到 key |
| 99995002 | 找不到 codex |
| 99995003 | 等锁超时 |
| 99995004 | codex 启动失败 |
| 99995005 | 上游 `/models` 出错 |
| 99995999 | 其他错误 |

acsw 自己出错时，stderr 最后一行还会有一行 JSON，比如 `{"acsw_error":99994001,"kind":"not_mapped","msg":"…","dir":"…"}`，给程序解析用。kind 字符串和详细说明见 README 的「退出码和错误 JSON」一节。
