[English](./README.md) | **简体中文**

<div align="center">

<a href="#快速开始"><img src="https://readme-typing-svg.demolab.com?font=Noto+Sans+SC&weight=600&size=24&pause=1000&color=56E656&center=true&vCenter=true&width=880&height=62&lines=%E4%B8%A4%E4%B8%AA%20Agent%EF%BC%8C%E4%B8%80%E4%B8%AA%E4%BB%93%E5%BA%93%EF%BC%8C%E4%B8%80%E6%AC%A1%E6%AD%BB%E9%94%81%E3%80%82%3B%E7%AD%89%E5%BE%85%E5%9B%BE%E4%BC%9A%E5%9C%A8%E5%BE%AA%E7%8E%AF%E7%AD%89%E5%BE%85%E5%BD%A2%E6%88%90%E4%B9%8B%E5%89%8D%E5%B0%B1%E6%8B%92%E7%BB%9D%E5%AE%83%E3%80%82" alt="两个 Agent，一个仓库，一次死锁。等待图会在循环等待形成之前就拒绝它。" /></a>

### Leasekeeper 是让并行 Coding Agent 不再互相死锁的租约仲裁器。

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Release](https://img.shields.io/badge/release-v0.1.0-56e656)](https://github.com/SuperMarioYL/leasekeeper/releases)
[![License](https://img.shields.io/badge/license-MIT-3fb950)](./LICENSE)
[![Platforms](https://img.shields.io/badge/platforms-linux%20%7C%20macOS%20%7C%20windows-8a93a5)](https://github.com/SuperMarioYL/leasekeeper/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/SuperMarioYL/leasekeeper/ci.yml?label=ci)](https://github.com/SuperMarioYL/leasekeeper/actions/workflows/ci.yml)

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/hero-dark.svg" />
  <source media="(prefers-color-scheme: light)" srcset="./assets/presentation/hero-light.svg" />
  <img src="./assets/presentation/hero-light.svg" alt="Leasekeeper —— 一个持有租约的闸门，另一个令牌正在靠近；下方一行字：阻止并行 Coding Agent 互相死锁。" width="100%" />
</picture>

</div>

---

## 为什么需要它

你在同一个检出上同时跑重构、测试和 GPU 任务。它们各自要一个文件、一个端口或一份配额，而没有任何机制要求它们在写入之前先取得独占所有权。两个各拿到一半资源的 Agent，可以互相等待到天荒地老 —— 你发现的时候，通常只是某个 Agent 突然不再产出。

常见的补救方式都是事后手工的：发现冲突、比对损失、重跑丢掉的工作，或者干脆放弃并行，把 Agent 串起来跑。

Leasekeeper 把所有权变成显式的，并让循环等待根本无法进入：

- **租约（lease）** 是某个命名资源上、有时限的独占所有权 —— 一个文件、一个端口、一块 GPU、一份配额。Agent 先获取，再持有，最后释放。
- **等待图（wait-for graph）** 记录谁在等谁。当某次 acquire 会闭合成环时，它会被直接拒绝并打印出环的路径，而不是让两个 Agent 一起阻塞到有人察觉。

后半句才是这个产品本身。按动作授权的守卫可以允许或隐藏某个文件，`flock` 可以覆盖某个进程树里的一个资源，但它们都不会构建那张能看见环的跨 Agent 关系图。

## 快速开始

本项目的正式仓库是 https://github.com/SuperMarioYL/leasekeeper。下面的命令就是为该仓库写的；在它上线之前，请在本地检出里执行同样的命令。

从源码构建。只需要一个 Go 工具链 —— 不需要任何配置，跑 demo 也不需要第二个终端。

```bash
git clone https://github.com/SuperMarioYL/leasekeeper.git
cd leasekeeper
go build -o leasekeeper ./cmd/leasekeeper
export PATH="$PWD:$PATH"   # 让示例脚本找到这个二进制
```

然后运行内置 demo。它会在一个临时 socket 上启动 broker，把两个 Agent 的循环等待从头演到尾：

```bash
leasekeeper demo
```

> **尚未发布。** 该模块还没有上公共代理，所以 `go install github.com/SuperMarioYL/leasekeeper/cmd/leasekeeper@latest` 在第一个带 tag 的 release 上线之前无法解析。在此之前请按上面的方式从源码构建。

想用两个真实的 Agent 进程、在两个终端里跑同一个故事，就用仓库自带的示例脚本：

终端 1 —— Agent a 先拿 `file:src/a.go`，然后等 `port:8080`：

```bash
leasekeeper run --name a -- ./examples/agent-a.sh
```

终端 2 —— Agent b 先拿 `port:8080`，然后去要 Agent a 手里的文件：

```bash
leasekeeper run --name b -- ./examples/agent-b.sh
```

也可以只用一个终端，让两个 Agent 共用一个 broker：

```bash
./examples/two-agents.sh
```

## 演示

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/demo.gif" />
  <img src="./assets/demo.gif" alt="终端录制：Agent a 拿到 file:src/a.go，Agent b 拿到 port:8080，a 等待该端口，而 b 请求 a 的文件时被拒绝，给出的环是 b -> a -> b。" width="100%" />
</picture>

这段录制的命令是 `leasekeeper demo --pause 3s` —— 与 `leasekeeper demo` 完全相同的一次运行，只是放慢了节奏，让每一步都看得清。它存在的意义在第四步，Agent b 的第二次 acquire 被拒绝：

```console
[b] acquire file:src/a.go
     acquire denied  acquire denied — would deadlock: b -> a -> b
```

被拒绝的 acquire 会以非零码退出，所以脚本可以据此分支，而不是一直卡住。

完整的录制转录、产生它的确切命令以及实现版本，都在 [`docs/demo-results.json`](./docs/demo-results.json) 里。

<details>
<summary><code>leasekeeper demo</code> 的完整录制输出</summary>

```console
$ leasekeeper demo
Leasekeeper demo — two agents, one resource pair, one refused cycle
broker started on a temporary socket; no configuration needed

  [a] acquire file:src/a.go
     granted  granted file:src/a.go to a

  [b] acquire port:8080
     granted  granted port:8080 to b

  [a] acquire port:8080   (blocks)
     waiting  waiting for port:8080 held by b

  [b] acquire file:src/a.go
     acquire denied  acquire denied — would deadlock: b -> a -> b

Leases
  RESOURCE       OWNER  HELD  TTL
  file:src/a.go  a      0s    -
  port:8080      b      0s    -

Wait-for graph
  a -> b

  [b] release port:8080
     released  released port:8080
     granted  granted port:8080 to a after waiting

Leases
  none held

Wait-for graph
  no agent is waiting on another

a held half the pair and b held the other half, so the second acquire was refused
instead of hanging. Nothing was preempted: the refusal failed fast, and once b
released the port the queued waiter was served in order.
```

</details>

## 工作原理

一个 Go 二进制，三种模式，没有数据库，也没有长期驻留的守护进程。

```
leasekeeper (single binary)
├── broker   每次运行会话一个进程；Unix socket；在内存中持有 Table + WaitGraph
├── client   CLI 与 Agent shim 共用的薄 RPC（acquire / release / status）
└── cli      run（启动 broker + 拉起子进程）· acquire · release · status · demo
```

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-dark.svg" />
  <source media="(prefers-color-scheme: light)" srcset="./assets/presentation/architecture-light.svg" />
  <img src="./assets/presentation/architecture-light.svg" alt="包结构：cmd/leasekeeper 调用 internal/cli，后者通过 internal/client 的 socket RPC 访问 internal/broker；broker 持有 internal/lease 与 internal/waitgraph。" width="100%" />
</picture>

broker 持有租约表和等待图，其余都只是调用方。`run` 在没有 broker 监听时启动一个，并包裹你的命令，于是 Agent 通过 `LEASEKEEPER_SOCKET` 就能访问它 —— 除了调用 `acquire` 和 `release`，不需要改动代码。

一次 acquire 会依次做这些事：

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/process-dark.svg" />
  <source media="(prefers-color-scheme: light)" srcset="./assets/presentation/process-light.svg" />
  <img src="./assets/presentation/process-light.svg" alt="acquire 的路径：声明资源、读取租约表、检查等待图，然后授予、排队或带着环被拒绝。" width="100%" />
</picture>

1. **声明资源。** 由调用方给出名字，不做任何自动发现。
2. **读取租约表。** 空闲就授予，并记录持有者。
3. **检查等待图。** 如果已被别人持有，让这个 Agent 等待会不会闭合成环？
4. **授予、排队或拒绝。** 安全的等待会被记录，Agent 排在持有者之后，严格先来先服务；会闭合环的等待被拒绝，并打印出环。

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-dark.svg" />
  <source media="(prefers-color-scheme: light)" srcset="./assets/presentation/integrations-light.svg" />
  <img src="./assets/presentation/integrations-light.svg" alt="broker 中枢连接四种可租用资源 —— file、port、gpu、quota —— 以及 run 包装器与 Unix socket。" width="100%" />
</picture>

## 用法

| 命令 | 作用 |
| --- | --- |
| `leasekeeper run -- <命令>` | 需要时启动 broker，带着 `LEASEKEEPER_SOCKET` 与 `LEASEKEEPER_AGENT` 运行命令，并在命令退出时交回它仍然持有的所有租约。 |
| `leasekeeper acquire <资源>` | 获取租约。会阻塞直到 Agent 真正拥有它，期间排在当前持有者之后。被拒绝时以非零码退出。 |
| `leasekeeper release <资源>` | 交回租约。只有持有者能释放，且最早排队的等待者会拿到它。 |
| `leasekeeper release --all` | 交回这个 Agent 持有的全部租约。 |
| `leasekeeper status` | 打印租约表和当前等待图。 |
| `leasekeeper demo` | 在临时 socket 上播放两个 Agent 的循环等待与拒绝。 |

资源名自带类型，因此文件路径永远不会和端口号冲突：

```
file:<path>     port:<n>     gpu:<id>     quota:<name>
```

```console
$ leasekeeper acquire file:src/a.go            # 阻塞直到拥有
$ leasekeeper acquire --ttl 5m port:8080       # 租约 5 分钟后失效
$ leasekeeper acquire --no-wait port:8080      # 报告持有者后直接返回
$ leasekeeper release file:src/a.go
$ leasekeeper status
```

### 参数与环境变量

| 参数 | 适用命令 | 含义 |
| --- | --- | --- |
| `--agent <名称>` | `acquire`、`release` | 租约归属的 Agent 身份。 |
| `--ttl <时长>` | `acquire` | 租约的有效期，例如 `5m`。`0` 表示持有到主动释放。 |
| `--no-wait` | `acquire` | 报告谁持有该资源后退出，而不是排队等待。 |
| `--timeout <时长>` | `acquire` | 等待多久后放弃，默认 `30s`。 |
| `--socket <路径>` | 全部 | broker socket。依次取 `$LEASEKEEPER_SOCKET`，再退回到临时目录下的按用户路径。 |
| `--no-color` | 全部 | 纯文本输出。输出被管道接管或设置了 `NO_COLOR` 时，颜色也会自动关闭。 |

| 环境变量 | 由谁设置 | 含义 |
| --- | --- | --- |
| `LEASEKEEPER_SOCKET` | `run` | 子 Agent 要访问的 socket。 |
| `LEASEKEEPER_AGENT` | `run` | 子 Agent 获取租约时使用的身份。 |

## 与现有方案对比

下面每种方案都真实地解决了问题的一部分，最后一行是它们都没有触及的那部分。

| 方案 | 它拥有什么 | 两个 Agent 想要同一个东西时会发生什么 |
| --- | --- | --- |
| `git worktree` | 每个 Agent 一棵独立文件树 | 文件不再互相踩，但只覆盖文件 —— 端口、GPU、配额都在它之外，而且文件冲突被推到了合并时。 |
| 按动作的守卫（允许或隐藏某次工具调用） | 什么也不拥有：授权是逐动作、无状态的 | 两个 Agent 依然可以各持有半个资源对，然后互相等待。 |
| `flock` 与文件锁 | 某个进程树里的一个资源 | 单个锁看不见横跨两个资源、两个 Agent 的环。 |
| **Leasekeeper** | 一个命名资源在一段有限时间内的所有权，外加等待图 | 第二次 acquire 被拒绝并打印出环；持有者释放后，排队者按顺序拿到资源。 |

## 限制

采用之前请先读这几条。它们是方案给 v0.1 划定的边界。

- **broker 是单机上每次运行会话一个本地进程。** 没有分布式租约，也没有跨机器协调。
- **必须由 Agent 自己，或者它的包装器，调用 `acquire` 与 `release`。** 对未修改 Agent 做内核级拦截（LD_PRELOAD、ptrace、seccomp）不在范围内，因此 `leasekeeper run -- <命令>` 就是那个接缝。
- **GPU 与配额是按约定租用的命名资源。** Leasekeeper 不会在驱动、CUDA 运行时或显存层面做隔离。
- **broker 退出时所有租约失效。** 这是有意为之：租约状态只在内存里，因此任何 Agent 都无法永久持有一个过期租约。
- **没有抢占，也没有优先级。** 被拒绝的 acquire 会快速失败，它不会把资源从当前持有者手里夺走。
- **没有自动发现。** 需要的资源由调用方自己声明。

## 路线图

**v0.1.0 已交付** —— file、port、gpu、quota 四类租约；`run`、`acquire`、`release`、`status`、`demo`；带环拒绝的等待图；TTL 过期；先来先服务的排队；按会话计数、最后一个 run 离开即关闭的 broker。

**尚未通过：需求验证闸门。** 方案在产品前面设了一道证伪闸门 —— 引用两条以上真实的资源冲突或循环等待事件，并访谈五位开发者，其中至少两位报告过真实损失。这道闸门**没有通过**：现有证据只停留在话题层面，没有引用任何具体事件。[`docs/validation.md`](./docs/validation.md) 记录了计数结果与访谈脚本，发布计划仍然被它挡在后面。本 README 不声称需求已被验证，因为这恰恰是尚未被证实的东西。

**闸门之后，而非之前。** 后续工作是候选方向，不是承诺，并且取决于那五场访谈的结果：对未修改 Agent 做内核级拦截，让它们无需改代码即可被覆盖；以及把 `quota:` 从一个名字变成可计量的预算。

## 许可证

<p align="center"><sub><a href="./LICENSE">MIT</a> © 2026 SuperMarioYL</sub></p>
