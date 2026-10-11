# MyCalendar

一个面向内网自部署的日历服务。Go 编写，SQLite 存储，只提供 HTTP API，不含前端。

设计目标是个人自用：**不做任何鉴权**，专注把事情做对——日程的增删改查、按日/周/月查询、以及正确的时区处理。数据模型只有 `events` 和 `reminders` 两张表。

> **安全提示**：本服务没有任何鉴权，所有接口都是公开的。请只监听在可信网络（或 `127.0.0.1`），对外暴露时务必在前面加一层带鉴权的反向代理。

---

## 目录

- [功能范围](#功能范围)
- [开发与运行环境版本](#开发与运行环境版本)
- [快速开始](#快速开始)
- [配置项](#配置项)
- [API 概览](#api-概览)
- [核心设计决策](#核心设计决策)
- [测试](#测试)
- [项目结构](#项目结构)
- [后续可做](#后续可做)

---

## 功能范围

已实现：

1. **HTTP 后端**，通过 API 完成日程的增、查、删、改。
2. **按日 / 周 / 月查询**，并且不是简单的 `SELECT` 后直接返回——查询基于**区间相交**而非"包含"，跨天日程会在它覆盖的每一天、每一周、每一月都出现。
3. **修改日程**，支持部分更新（PATCH），字段语义区分"未提供"、"置空"和"赋值"。
4. **时区处理**：存储与解释解耦，`tz` 参数按请求决定"这一天"的边界。
5. **提醒（reminders）**：可写入、随日程返回，但**不发送**任何通知。
6. **根节点 `/help`**：完整的 API 文档，JSON 或 Markdown 两种形态，供人类和 agent 阅读。

不包含（有意为之）：提醒推送、单独提醒接口、重复日程、跨多天的全天事件、鉴权/多用户、全文检索、前端。

---

## 开发与运行环境版本

下表是**实际用于开发与验证**的版本。为避免依赖漂移导致的问题，建议保持一致；`go.mod` 中 `go` 指令声明的是最低要求（`1.26.0`），等于或高于它的工具链均可构建。

| 项目 | 版本 | 说明 |
|---|---|---|
| 操作系统 | Debian GNU/Linux 12 (bookworm) | 开发环境 |
| 架构 | linux/amd64 (x86_64) | |
| **Go 工具链** | **go1.27.2** | 官方稳定版；`go.mod` 最低要求 `go 1.26.0` |
| **SQLite 驱动** | **modernc.org/sqlite v1.60.1** | 纯 Go 实现，**无需 CGO** |
| SQLite 引擎版本 | 3.53.4 | 由上述驱动内嵌，运行时可用 `select sqlite_version()` 查看 |
| CGO | 不需要（`CGO_ENABLED=0` 可正常构建运行） | 便于交叉编译到内网目标机 |
| HTTP 框架 | 标准库 `net/http` | 使用 Go 1.22+ 的 `ServeMux` 方法 + 通配符路由，无第三方路由依赖 |
| 时区数据库 | 内嵌 `time/tzdata` | 部署机不需要安装 tzdata |
| 测试 | 标准库 `testing` + `net/http/httptest` | 无第三方断言库 |

### 依赖清单

直接依赖只有一个（`go.mod` 中其余均为其间接依赖）：

```
github.com/lyy1119/MyCalendar
└── modernc.org/sqlite v1.60.1   (直接依赖)
    ├── modernc.org/libc v1.77.1
    ├── modernc.org/mathutil v1.7.1
    ├── modernc.org/memory v1.12.1
    ├── github.com/dustin/go-humanize v1.0.1
    ├── github.com/google/uuid v1.6.0
    ├── github.com/mattn/go-isatty v0.0.24
    ├── github.com/ncruces/go-strftime v1.0.0
    ├── github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec
    └── golang.org/x/sys v0.48.0
```

**为什么选 `modernc.org/sqlite` 而不是 `mattn/go-sqlite3`**：两者都是成熟社区库，但前者是纯 Go 实现，不需要 CGO 与 gcc，交叉编译到不同架构的内网目标机时无需准备 C 交叉工具链，产物是单个静态可执行文件。代价是构建时间略长、二进制体积略大，对个人日历场景没有实际影响。

---

## 快速开始

```bash
# 构建
go build -o mycalendar ./cmd/mycalendar

# 运行（默认监听 :8080，数据库为当前目录下的 mycalendar.db）
./mycalendar

# 指定时区与地址
MYCAL_TZ=Asia/Shanghai MYCAL_ADDR=127.0.0.1:8080 MYCAL_DB=/var/lib/mycalendar/data.db ./mycalendar
```

查看文档：

```bash
# JSON 版（供程序解析）
curl -s http://127.0.0.1:8080/help

# Markdown 版（供人阅读）
curl -s -H 'Accept: text/plain' http://127.0.0.1:8080/help
```

最小示例：

```bash
# 新建日程
curl -s -X POST 'http://127.0.0.1:8080/api/v1/events?tz=Asia/Shanghai' \
  -H 'Content-Type: application/json' \
  -d '{"title":"牙医","start_at":1773205200,"end_at":1773208800}'

# 查今天
curl -s 'http://127.0.0.1:8080/api/v1/events?tz=Asia/Shanghai'

# 查某一天
curl -s 'http://127.0.0.1:8080/api/v1/events?range=day&date=2026-03-11&tz=Asia/Shanghai'

# 查某一周 / 某一月
curl -s 'http://127.0.0.1:8080/api/v1/events?range=week&date=2026-03-11&tz=Asia/Shanghai'
curl -s 'http://127.0.0.1:8080/api/v1/events?range=month&date=2026-03-11&tz=Asia/Shanghai'

# 修改：把任务标记为已完成
curl -s -X PATCH 'http://127.0.0.1:8080/api/v1/events/1' \
  -H 'Content-Type: application/json' -d '{"is_completed":true}'

# 删除
curl -s -X DELETE -o /dev/null -w '%{http_code}\n' 'http://127.0.0.1:8080/api/v1/events/1'
```

---

## 配置项

全部通过环境变量配置，均可省略。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `MYCAL_ADDR` | `:8080` | HTTP 监听地址。内网自用建议 `127.0.0.1:8080` 或明确的网卡地址。 |
| `MYCAL_DB` | `mycalendar.db` | SQLite 文件路径。**首次运行会自动创建数据库文件；若其所在目录不存在，也会自动创建**（目录权限 `0750`）。路径中不能包含 `?` 或 `#`（会与 DSN 语法冲突，启动时明确报错）。 |
| `MYCAL_TZ` | `Local` | 服务默认时区（IANA 名称，如 `Asia/Shanghai`）。请求可用 `tz` 参数覆盖。 |
| `MYCAL_WEEK_START` | `monday` | 周视图起始日，`monday` 或 `sunday`。 |
| `MYCAL_CORS_ORIGINS` | `*` | 允许的跨域来源，逗号分隔；`*` 表示任意来源。 |
| `MYCAL_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`。 |
| `MYCAL_TRUST_PROXY` | `false` | 预留，用于反向代理场景。 |

启动时会打印生效的地址、数据库路径、时区与周起始日，便于确认。日志中的数据库路径是**绝对路径**，避免相对路径带来的"文件到底建在哪"的疑问。

关于数据库路径的几点行为：

- 数据库文件不存在时自动创建（含建表与索引），无需手工初始化。
- 数据库文件所在的**目录不存在时也会自动创建**（多级目录一并创建）。SQLite 自身只创建文件、不创建目录，因此这一步由服务完成，避免首次以 `/var/lib/mycalendar/data.db` 这类路径启动时报出含义不明的 `unable to open database file`。
- 若给定路径本身是一个已存在的目录，或路径上级存在同名普通文件，启动会失败并给出明确提示（告诉你应该给文件路径，而不是一个目录）。
- `MYCAL_DB=:memory:` 可用于一次性验证，数据不落盘。

---

## API 概览

完整且详尽的文档在运行时的 **`GET /help`**——那里是与实现同步的唯一权威版本（测试会校验文档列出的接口真实存在、以及文档中的限额与代码常量一致）。下表只是索引。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/` | 服务信息与接口索引 |
| GET | `/help` | 完整 API 文档（JSON；`Accept: text/plain` 返回 Markdown） |
| GET | `/healthz` | 存活与数据库健康检查 |
| POST | `/api/v1/events` | 新建日程 |
| GET | `/api/v1/events` | 按日/周/月或自定义区间查询 |
| GET | `/api/v1/events/{id}` | 查询单个日程（含提醒） |
| PATCH | `/api/v1/events/{id}` | 部分更新 |
| DELETE | `/api/v1/events/{id}` | 删除日程（级联删除提醒） |

约定：时间一律为 **Unix 秒**；日历日期为 `YYYY-MM-DD` 字符串；所有区间**左闭右开** `[start, end)`；请求体中出现**未定义字段会直接报错**（避免拼写错误被静默忽略）；错误统一为 `{"error":{"code":...,"message":...,"details":[...]}}`。

---

## 核心设计决策

这几条是阅读代码前最需要知道的，也是 `/help` 里花最多篇幅解释的部分。

### 1. 查询是"相交"而不是"包含"

一个 `23:00 → 次日 01:00` 的日程会同时出现在两天的查询结果里；一个跨三天的日程会在它覆盖的每一天、每一周、每一月都出现（同一次查询中每条只出现一次）。

SQL 中为每个日程计算其**占用区间**，再与查询窗口求交：

```sql
start_at < :range_end AND COALESCE(end_at, start_at + 1) > :range_start
```

`end_at` 为 `NULL` 时按"1 秒的瞬时事件"处理，这样恰好落在窗口第一秒的日程不会被漏掉（只用 `end_at > :range_start` 会漏）。

### 2. 全天日程用日期定位，定时日程用时间戳定位

这是两套坐标系：

- **定时日程**：由 `start_at`/`end_at` 两个绝对瞬时定位，按时间戳求交。
- **全天日程**：由 `event_date` 这个**日历日期**定位，按日期字符串比较。

为什么不全用时间戳？因为全天日程的 `start_at`/`end_at` 只是"该日期在某个时区下的渲染值"。若拿它去和另一个时区的窗口求交，会出现跨时区误判（例如按上海时间写入的 3 月 31 日全天日程，在 UTC 下查询 3 月 31 日时可能被判成落在 4 月 1 日）。而**日期在任何时区都是同一个日期**，所以按 `event_date` 比较可让全天日程的结果在任何 `tz` 下保持稳定。

代价是：schema 中全天日程只有一个日期字段，因此**无法表达跨多天的全天事件**；需要跨天时请改用定时日程。

### 3. 时区：存储与解释分离

- 存储层完全时区无关：`start_at` 等是绝对 Unix 秒，`event_date` 是纯日期。
- `tz` 参数（IANA 名称）只影响**本次请求**把瞬时投影到哪一天/周/月，**从不改写数据**。
- 日/周/月的边界由**当地墙上时间的零点**推导（`time.Date(y, m, d, 0, 0, 0, 0, loc)`），而不是把 24 小时当作一天。因此夏令时切换当天是 23 或 25 小时、周窗口是 167 或 169 小时——测试有专门覆盖。
- 二进制内嵌 IANA 时区数据库（`time/tzdata`），部署机不装 tzdata 也能正确解析 `Asia/Shanghai` 这类时区名。

### 4. `event_date` 与 `is_all_day` 联动

两者始终保持一致，避免"字段刚被置空又被自动推导回来"这种怪现象：

- 传入非空 `event_date` 且未显式指定 `is_all_day` → `is_all_day` 变为 `true`（定时转全天）。
- 传入 `event_date: null` 且未显式指定 `is_all_day` → `is_all_day` 变为 `false`（全天转回定时）。
- 显式传入的 `is_all_day` 优先级最高。
- 全天日程若只给 `is_all_day: true` 而未给日期，则用 `start_at` 在**请求时区**下反推日期；同一瞬时在不同 `tz` 下会落到不同日期，这是符合预期的。

全天日程的 `start_at`/`end_at` 会被规范化为"当地零点 → 次日当地零点"，实际日期以 `event_date` 为准。

### 5. PATCH 的三种语义

用 `Optional[T]` 区分 JSON 中"字段缺失"与"字段为 null"：

| 请求体 | 效果 |
|---|---|
| 字段缺失 | 保持原值不变 |
| `"notes": null` | 清空该字段（`notes`、`end_at`、`event_date` 可清空） |
| `"notes": "x"` | 赋新值 |

`{}` 是合法的空操作（仅刷新 `updated_at`）。响应始终返回**规范化之后的完整日程**，客户端不会拿到中间态。`title` 与 `start_at` 不允许为 `null`。

另外：`is_task` 为 `false` 时 `is_completed` 会被强制存为 `false`（完成状态对非任务没有意义）；`reminders` 是**整体替换**而非追加，传 `[]` 或 `null` 即清空，省略则不动。

### 6. 提醒只存不发

`reminders` 表记录 `remind_at`，随所属日程一起返回（按 `remind_at` 排序）。服务本身**不调度、不推送、不发邮件**——它只是存储与读取接口，由客户端读取 `remind_at` 后自行处理。删除日程时提醒通过 `ON DELETE CASCADE` 一并删除。

---

## 测试

```bash
go test ./...                 # 全部测试
go test -race -count=1 ./...  # 竞态检测
go vet ./...                  # 静态检查
gofmt -l .                    # 应为空
```

测试使用临时目录中的独立数据库，不依赖外部服务，也不依赖宿主机时区（时区相关用例显式指定 `Asia/Shanghai` / `America/New_York` / `UTC` 等）。

覆盖重点：

- **时间窗口**：当地零点、夏令时 23/25 小时的一天、167 小时的周、跨年月份、闰年 2 月、`2026-02-30` 这类非法日期。
- **相交语义**：跨天日程、跨午夜日程、恰好结束于窗口起点的日程（不应命中）、无 `end_at` 的瞬时日程恰好落在窗口第一秒（应命中）。
- **全天日程的时区稳定性**：同一个全天日程在上海、UTC、纽约、Kiritimati、Midway 下的"当天"查询都能命中。
- **时区只影响解释**：同一瞬时在 UTC 与上海下落到不同日期，且数据未被改写。
- **PATCH 语义**：缺省/置空/赋值三态、`{}` 空操作、全天与定时相互转换、`created_at` 不变而 `updated_at` 刷新。
- **校验**：标题与备注长度上限（按字符计，CJK 友好）、提醒数量上限与去重排序、时间戳越界与"毫秒当成秒"的识别提示、非法 `tz`/`range`/`date`/`from,to` 组合。
- **HTTP 层**：状态码与错误信封、未定义字段拒绝、带 `Allow` 头的 405、未知路径的 JSON 404、CORS 预检、`/help` 文档与实际路由及限额一致。
- **并发**：24 个 goroutine 并发写入不出现 `database is locked`。

---

## 项目结构

```
cmd/mycalendar/          程序入口：配置加载、优雅退出、HTTP 服务装配
internal/api/            HTTP 层：路由、中间件、请求解析、错误映射
  ├── api.go             服务器装配、中间件（恢复/日志/CORS）、响应与错误信封
  ├── query.go           查询参数解析（tz、range/date、from/to、过滤、分页）
  ├── events.go          日程各接口的处理器
  └── help.go            /help 文档的数据结构与 Markdown 渲染
internal/service/        领域层：校验与规范化（"字段该如何理解"都在这里）
internal/store/          SQLite 持久化：建表/迁移、增删改查、区间查询
internal/calendar/       时区相关的日历窗口计算（日/周/月/自定义）
internal/model/          与表结构一一对应的类型，以及 Optional / EventView
internal/config/         环境变量配置
internal/apperr/         错误分类（校验错误、参数错误、未找到）
```

数据表（与需求给定的一致；实现只额外加了索引）：

- `events`：`id, title, notes, start_at, end_at, is_all_day, event_date, is_task, is_completed, created_at, updated_at`
- `reminders`：`id, event_id, remind_at, created_at`

索引：`events(start_at)`、`events(end_at)`、`events(event_date)`、`events(is_all_day, is_task, is_completed)`、`reminders(event_id)`、`reminders(remind_at)`。

建表通过 `PRAGMA user_version` 做版本化迁移，重复启动幂等；若数据库版本高于当前二进制支持的版本，会拒绝启动而不是冒险运行。

SQLite 连接使用 WAL 日志模式、`busy_timeout=5000`，并为每个连接启用外键约束（`foreign_keys` 是连接级设置，因此写在 DSN 里以确保连接池中每个连接都生效）——这也是删除日程能级联清理提醒的前提。

---

## 后续可做

- 提醒的实际调度与推送（当前只存不发）。
- WebSocket / SSE 推送变更，供客户端实时刷新。
- 重复日程（需要在表上增加 recurrence 规则字段）。
- 跨多天的全天事件（需要为全天引入结束日期字段）。
- 前端界面。
