# MyCalendar

一个面向内网自部署的日历服务：Go 后端 + React 前端，SQLite 存储，单个二进制即可运行。

设计目标是个人自用：**不做任何鉴权**，专注把事情做对——日程的增删改查、按日/周/月查询、以及正确的时区处理。数据模型只有 `events` 和 `reminders` 两张表。

> **安全提示**：本服务没有任何鉴权，所有接口都是公开的。请只监听在可信网络（或 `127.0.0.1`），对外暴露时务必在前面加一层带鉴权的反向代理。

---

## 目录

- [功能范围](#功能范围)
- [开发与运行环境版本](#开发与运行环境版本)
- [快速开始](#快速开始)
- [直接运行二进制](#直接运行二进制)
- [配置项](#配置项)
- [API 概览](#api-概览)
- [前端](#前端)
- [核心设计决策](#核心设计决策)
- [Docker 部署](#docker-部署)
- [版本与数据迁移](#版本与数据迁移)
- [测试](#测试)
- [项目结构](#项目结构)
- [后续可做](#后续可做)

---

## 功能范围

已实现：

1. **HTTP 后端**，通过 API 完成日程的增、查、删、改。
2. **按日 / 周 / 月查询**，并且不是简单的 `SELECT` 后直接返回——查询基于**区间相交**而非"包含"，跨天日程会在它覆盖的每一天、每一周、每一月都出现。
3. **修改日程**，支持部分更新（PATCH），字段语义区分"未提供"、"置空"和"赋值"。
   **提醒（reminders）既可随日程整体写入，也可通过独立接口单独增删改查**。
4. **时区处理**：存储与解释解耦，`tz` 参数按请求决定"这一天"的边界。
5. **提醒（reminders）**：独立的增删改查接口，可随日程整体写入，但**不发送**任何通知。
6. **数据库自动迁移**：启动时检查格式版本并按主版本迁移，只支持逐个大版本升级，**详见[版本与数据迁移](#版本与数据迁移)**。
7. **根节点 `/help`**：完整的 API 文档，JSON 或 Markdown 两种形态，供人类和 agent 阅读。

**前端**：React + FullCalendar，参考 Google Calendar 的深色版式，包含「4 天」与「周」两种视图、置顶的全天日程行、可滚动时间网格，点击日程即可编辑。

不包含（有意为之）：提醒推送、单独提醒接口、重复日程、跨多天的全天事件、鉴权/多用户、全文检索。

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
| **Node.js** | **v24.20.0** | 仅构建前端时需要；后端不受影响 |
| **npm** | **11.19.0** | 仓库内含 `package-lock.json`，请用 `npm ci` |
| **React** | **19.3.0** | 前端框架 |
| **FullCalendar** | **6.1.21** | 日历组件（core / react / timegrid / daygrid / interaction，版本必须一致） |
| **Vite** | **8.3.4** | 前端构建工具 |
| **TypeScript** | **5.9.3** | 类型检查 |
| 浏览器验证（可选） | Playwright + Chromium | 仅 `web/e2e` 使用，不参与常规构建 |

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
# 构建前端 + 后端，产出内嵌了界面的单个二进制
make build

# 只构建后端（不需要 Node 工具链，但不含界面）
make build-backend

# 运行（默认监听 :8080，数据库为当前目录下的 mycalendar.db）
./mycalendar

# 指定时区与地址
MYCAL_TZ=Asia/Shanghai MYCAL_ADDR=127.0.0.1:8080 MYCAL_DB=/var/lib/mycalendar/data.db ./mycalendar
```

打开 <http://127.0.0.1:8080/> 即是日历界面。前后端分离开发时用 `make dev`：后端在 :8080，Vite 开发服务器在 :5173 并把 `/api`、`/help`、`/healthz` 代理到后端，改前端代码即时热更新。

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

## 直接运行二进制

不装 Docker 也能用。每个 release 都附带两个免安装可执行文件，**前端与 SQLite 都已内嵌，不依赖任何运行库**：

| 文件 | 平台 |
|---|---|
| `mycalendar-<版本>-linux-amd64.tar.gz` | Linux x86-64 |
| `mycalendar-<版本>-windows-amd64.zip` | Windows x86-64 |

```bash
# Linux：解压后直接跑（归档保留了可执行位）
tar -xzf mycalendar-1.1.2-linux-amd64.tar.gz
MYCAL_TZ=Asia/Shanghai MYCAL_DB=/var/lib/mycalendar/data.db ./mycalendar

# Windows：解压出 mycalendar.exe 双击或命令行运行
#   set MYCAL_TZ=Asia/Shanghai
#   mycalendar.exe
```

启动后打开 <http://127.0.0.1:8080/>。几个要点：

- 两个平台的二进制都是静态的：Linux 侧不依赖 libc，Windows 侧不需要任何 DLL。原因是 SQLite 驱动是纯 Go、时区库（`time/tzdata`）内嵌，构建时 `CGO_ENABLED=0`
- 配置全部走环境变量，与容器镜像完全一致（见「配置项」）
- 数据库默认落在**当前工作目录**的 `mycalendar.db`；建议显式指定 `MYCAL_DB` 到一个专门目录。目录不存在会自动创建
- **Windows 上没有 `/etc/localtime` 的等价物，请务必显式设置 `MYCAL_TZ`**，否则时区会退回 UTC（Windows 自身能解析具名时区，只是默认值不是你期望的那个）
- 只构建 x64。需要别的架构就用 `go build` 自己编，或用容器镜像

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
| GET | `/` | 日历前端（单页应用，兜底任何非 API 路径） |
| GET | `/api` | 服务信息与接口索引（JSON，供程序/agent 使用） |
| GET | `/help` | 完整 API 文档（JSON；`Accept: text/plain` 返回 Markdown） |
| GET | `/healthz` | 存活与数据库健康检查 |
| POST | `/api/v1/events` | 新建日程 |
| GET | `/api/v1/events` | 按日/周/月或自定义区间查询 |
| GET | `/api/v1/events/{id}` | 查询单个日程（含提醒） |
| PATCH | `/api/v1/events/{id}` | 部分更新 |
| DELETE | `/api/v1/events/{id}` | 删除日程（级联删除提醒） |
| GET | `/api/v1/reminders` | 查询提醒（可按 `event_id` 与时间窗过滤——客户端轮询"该提醒了"用它） |
| POST | `/api/v1/reminders` | 给已存在的日程添加提醒 |
| GET | `/api/v1/reminders/{id}` | 查询单个提醒 |
| PATCH | `/api/v1/reminders/{id}` | 修改提醒时间 |
| DELETE | `/api/v1/reminders/{id}` | 删除单个提醒（不动日程） |

日程字段包含 `title`、`notes`、`location`（地点）、起止时间、`is_all_day`、`is_task`、`is_completed` 与 `reminders`；约定：日历日期为 `YYYY-MM-DD` 字符串；所有区间**左闭右开** `[start, end)`；请求体中出现**未定义字段会直接报错**（避免拼写错误被静默忽略）；错误统一为 `{"error":{"code":...,"message":...,"details":[...]}}`。

时间戳的**输入**有三种等价写法，**存储**始终是 UTC 基准的 Unix 秒，不会因为你用哪种写法而改变：

| 写法 | 例子 | 含义 |
|---|---|---|
| Unix 秒 | `1773205200` | 绝对瞬时 |
| 带偏移的 RFC3339 | `"2026-03-11T13:00:00+08:00"` | 绝对瞬时，与 `tz` 无关 |
| 不带偏移的本地时间 | `"2026-03-11T13:00"` | **按本次请求的 `tz` 解释** |

响应里除了 `start_at`/`end_at`（Unix 秒）之外，还给出 `start_at_local`/`end_at_local`（同一瞬时在本次请求 `tz` 下渲染的 RFC3339）。`from`/`to` 查询参数接受同样的三种写法。

路由分工：`/` 归前端（任何非 `/api` 开头的路径都会返回前端外壳，这样前端路由刷新不会 404），`/api/*` 归机器，未知的 `/api/*` 路径仍然返回 JSON 404 而不会被前端吞掉。JSON 版的接口索引位于 `/api`（原先在 `/`，前端接管根路径后迁移）。

---

## 前端

`web/` 是 React + TypeScript 单页应用，用 **FullCalendar 6** 承担日历渲染。构建产物由 Vite 输出到 `internal/webui/dist/`，再通过 `go:embed` 打进二进制——所以内网部署只需要拷一个文件，也不存在跨域配置。

### 视图与交互

| 能力 | 实现 |
|---|---|
| 4 天视图 | 默认视图。FullCalendar 的自定义 `timeGrid` 视图（`duration: { days: 4 }`），免费版即可定义，不需要额外插件包 |
| 周视图（本周） | `timeGridWeek`；`今天` 按钮回到当前周 |
| 全天日程置顶 | `allDaySlot`，全天日程渲染在**日期表头下方、时间网格上方**的独立行里，不随时间滚动 |
| 可滚动时间网格 | 日期表头与全天行固定，其下是**一个**滚动区，左时间轴固定、各天列共用该滚动区，因此各列小时线始终对齐（与截图一致） |
| 点击日程编辑 | `eventClick` 打开编辑弹窗 |
| 提醒时间 | 弹窗内可增、改、删多个提醒；另有「提前 5/10/30 分钟、1 小时、1 天」快捷档位，按日程开始时间算出绝对时间填入 |
| 外观 | 自动 / 亮色 / 暗色，见下 |
| 日程块内容 | 正常高度时从上到下是：**标题 → 时间 → 地点 → 备注**。块太矮、只放得下一行时，改成**「标题, 时间」同处一行** —— 标题在两种排布里都在最前（它决定这是哪个日程），同行时用逗号分隔，时间保证完整显示（被截断的时间没有意义），由标题承担省略号截断；此时地点与备注不显示。全程不撑开日程块——块的大小由时长决定，放不下的整行裁掉而不是把文字压扁。全天日程是单行横条，地点与标题并排、备注不显示 |
| 完成的任务 | 亮绿色（`#34d399`）低透明度填充 + 同色左边条 + 删除线，既不刺眼又一眼可辨 |
| 编辑开关 | 左侧「外观」下方的开关，默认锁定；锁定时点击已保存的日程不会打开编辑框，但「新建日程」始终可用 |
| 滚动条 | 时间网格的纵向滚动条已隐藏（滚轮/触控板/键盘仍可滚动），避免它紧贴最后一天列显得像网格的一部分 |
| 新建 | 在网格空白处**拖选**时间段，或点击全天行，或点侧栏「新建日程」 |
| 增删查改 | 编辑弹窗内保存 / 删除，全部走既有 HTTP API |

时间粒度 1 小时、每小时 48px，`scrollTime` 默认从 07:00 开始；有「当前时间」红线指示。

刻意**没有**做拖拽改期：编辑统一走弹窗，避免一次误拖就改掉数据。

### 时区

前端用**浏览器的 IANA 时区**（`Intl.DateTimeFormat().resolvedOptions().timeZone`）作为 `tz` 参数发给 API；日历本身则使用 FullCalendar 的默认时区 `local`（即浏览器本地时区），两者因此天然一致。

> **不要给 FullCalendar 传 `timeZone="Asia/Shanghai"` 这类具名时区。** `@fullcalendar/core` 自身并不带具名时区实现（`namedTimeZoneImpl` 必须由插件提供），传了具名时区会**静默退化为 UTC**——不报警告，但所有日程都会被画到错误的时刻（东八区差 8 小时），`datesSet` 交回的窗口边界也会变成 UTC 零点，导致跨午夜的日程取不到。这个问题在 UTC 环境下测试时完全看不出来，所以浏览器端验证固定跑在 `Asia/Shanghai`。

取数窗口用 `datesSet` 的 `startStr`/`endStr`（带偏移的 ISO 字符串），而不是 `start`/`end` 这两个 Date 对象：后者是"UTC 零点的标记值"，直接 `.getTime()` 会把窗口整体平移一个时区偏移。

整天日程仍以 `event_date` 字符串传递，不经过 `Date`，因此不会被时区影响。

另外两个钩子的语义不同，不能混用取值方式：`dayHeaderContent` 的 `arg.date` 是**已转换到日历时区的真实 Date**（用 `getDate()`），而 `dayCellContent` 的 `arg.date` 是**原始的 UTC 零点标记**（用 `getUTCDate()`）。用错会让日期整体差一天。

### 与 API 的字段映射

| 侧 | 表示 |
|---|---|
| API | `start_at`/`end_at` 为 Unix 秒；全天日程用 `event_date`（`YYYY-MM-DD`）定位 |
| FullCalendar | 定时日程用 `Date` 对象，全天日程用日期字符串 |
| 编辑弹窗 | 原生 `datetime-local` / `date` 输入框的字符串 |

全天开关与 `event_date` 联动（与服务端规则一致）：打开全天则用日期、丢掉时间；关闭全天则把日期还原成 09:00–10:00，而不是午夜。

另外：`end_at` 为 `null` 的日程在服务端语义上是「一个瞬时」，网格里会给它画 15 分钟的小块以免看不见——**这只是显示，不会写回数据库**。

### 外观

左上侧栏「说明」下方有外观切换：**自动 / 亮色 / 暗色**。自动模式下跟随系统的 `prefers-color-scheme`，并且系统切换时会实时跟随（不只是加载时读一次）；选择会记在 `localStorage`。

实现上，两套配色都写成 `data-theme` 属性选择器下的 CSS 变量，由 JS 解析出最终主题后写到根元素上。`index.html` 里有一段同步执行的内联脚本做同样的解析，**在首次绘制前**就把属性设好，否则刷新时会闪一下错误的配色。

FullCalendar 的 `--fc-*` 变量全部映射到同一组变量，所以网格会跟着切换，不会出现"外面亮了、日历还是黑的"。

### 构建

```bash
make web-install   # cd web && npm ci
make web           # 类型检查 + 打包到 internal/webui/dist
make build         # 上面两步 + go build -tags webui_embedded
```

`webui_embedded` 是构建标签：不带该标签时 `internal/webui` 仍能编译，只是访问 `/` 会返回一页说明（告诉你如何构建前端），因此**没有 Node 环境也能 `go build` 和 `go test`**。构建产物不入库，`.gitignore` 已忽略 `internal/webui/dist/`。

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

另外：`is_task` 为 `false` 时 `is_completed` 会被强制存为 `false`（完成状态对非任务没有意义）；`reminders` 是**整体替换**而非追加，传 `[]` 或 `null` 即清空，省略则不动——弹窗里删掉最后一个提醒时正是靠提交空数组来清空。

### 6. 提醒只存不发，但有完整的增删改查

**存储与检索是完整的**，只有"把提醒真正发出去"这件事不做：服务不调度、不推送、不发邮件、不与前端做任何提醒投递通信。客户端（Android app 或 agent）自己轮询 `/api/v1/reminders` 取到期的提醒再决定怎么处理。

两条写入路径，落库结果完全相同，按场景选用：

| 路径 | 语义 | 适用 |
|---|---|---|
| 日程载荷里的 `reminders` 数组 | **整体替换**，与日程同一次请求、同一事务 | 创建日程时一并带上；或一次改完整个集合 |
| `/api/v1/reminders` 独立接口 | 单条增删改 | 只动一条提醒、不想碰日程；客户端按 id 追踪提醒 |

独立接口的行为：`POST` 时若该日程在同一时刻已有提醒，返回 **409 `reminder_exists`**（两条完全相同的记录无法区分）；`PATCH` 落到同日程已占用的时刻同理。日程载荷那条路径则把重复时刻静默合并。每个日程上限 50 条，两条路径都受限。

`remind_at` 是**绝对瞬时**，和 `start_at` 一样接受三种写法（Unix 秒 / 带偏移的 RFC3339 / 不带偏移的本地时间按 `tz` 解释）。API **没有**"提前 N 分钟"这种相对写法——那是 UI 的便利，界面上提供了几个常用档位，实际提交的仍是算好的绝对时间。删除日程时提醒通过 `ON DELETE CASCADE` 一并删除；删除提醒不影响日程。

---

## Docker 部署

三阶段构建：Node 编译前端 → 前端产物 `go:embed` 进 Go 二进制 → **只把二进制放进 `scratch`**。最终镜像里没有 shell、没有包管理器、没有 libc、没有 `time/tzdata` 之外的任何时区文件。

```bash
# 构建
docker build -t mycalendar .

# 运行（数据放在命名卷里，重建容器不丢）
docker run -d --name mycalendar -p 8080:8080 -v mycalendar-data:/data mycalendar

# 或者直接用 CI 发布的镜像（不用本地编译，也不会卡在 go mod download）
docker run -d --name mycalendar -p 8080:8080 -v mycalendar-data:/data \
  ghcr.io/lyy1119/mycalendar:latest

# 或者用 compose：先从模板复制一份，见下
cp docker-compose.yml.template docker-compose.yml
docker compose up -d --build
```

打开 <http://127.0.0.1:8080/>。

### 用 compose 部署

仓库里只版本化**模板** `docker-compose.yml.template`，真正的 `docker-compose.yml` 在 `.gitignore` 里。所以先复制一份再改：

```bash
cp docker-compose.yml.template docker-compose.yml
${EDITOR:-vi} docker-compose.yml   # 按需改宿主目录、时区、版本号

make deploy        # 先拉取 CI 发布的镜像，拉不到就本地构建
docker compose logs -f              # 看启动日志，确认生效的地址/数据库/时区
```

**默认是从 ghcr 拉取已发布的镜像**（`pull_policy: missing`：本地有就用本地的，没有就拉）。拉取需要能访问 ghcr 并具备该包的读取权限；拿不到就走 `make deploy` 的本地构建分支，不影响使用。

**「拉取失败就本地构建」这件事 Compose 自己做不到。** 它的 `pull_policy` 只有 `always` / `never` / `missing` / `build` 四个值，没有「先拉、拉不到再构建」；直接 `docker compose up` 在拉取失败时就是报错退出——这正是你遇到的那个现象。所以这一步放在了 `make deploy` 里：

```bash
docker compose pull --quiet && docker compose up -d   # 拉到了就用镜像
# 否则
docker compose up -d --build                          # 拉不到就本地编译
```

模板里的 `build:` 段是为此保留的。如果你不想碰 registry，把 `pull_policy` 改成 `build` 即可完全本地构建；想每次都去 registry 确认最新，改成 `always`。

**为什么是模板而不是直接给一份 `docker-compose.yml`**：部署相关的改动（数据库放哪个宿主目录、时区、版本号、端口）都是本机的事，不该进版本库，也不该在每次 `git pull` 时冲突。把模板入库、把实际文件忽略掉，`git pull` 就永远不用管它。

模板里几处常用的改动点都写了注释：

| 想改什么 | 改模板里的哪一段 |
|---|---|
| 数据库落到宿主机目录 | `volumes:` 里换成绑定挂载那一行（见下一节） |
| 端口 | `ports:` |
| 时区 | `MYCAL_TZ`（镜像默认已是 `Asia/Shanghai`） |
| 版本号 | `build.args.VERSION`，或构建时传 `BUILD_VERSION=`（见后） |
| 跨域来源 | `MYCAL_CORS_ORIGINS`，默认 `*`；本服务无鉴权，谨慎放宽 |

`.gitignore` 忽略 `docker-compose.yml` 还有个附带好处：万一你把宿主路径、内网地址甚至口令写进了 `environment:`，也不会被 `git add` 带上去。

### 镜像发布（CI）

`.github/workflows/release.yml` 在每次推送到 `main` 时自动完成：跑测试 → 算出下一个版本号 → 构建镜像推到 ghcr.io → 打 git tag → 建 GitHub Release。

**版本号是自动的**：`.github/scripts/next-version.sh` 读取仓库里已有的稳定 `vX.Y.Z` tag，**把第三位 +1**（`v0.1.2` → `v0.1.3`）。没有任何 tag 时从 `v0.1.0` 起步——项目还没到 1.0，凭空做 patch 递进会误导。只认稳定的 `vX.Y.Z`，预发布和随手打的 tag（比如 `v2`、`v0.11.0-beta`）会被忽略，免得它们把版本号带偏。

> 有个坑值得说明：`git tag --sort=-v:refname` 会把 `v2`、`v0.11.0-beta`、`v0.10.1-rc.1` 排在稳定的 `v0.10.0` **前面**，所以脚本必须先用正则过滤再取第一条，否则会拿 `v2` 去算。这一条已用一组 tag 状态实测过。

镜像标签有三种：`<版本号>`（如 `0.1.3`）、`latest`、`sha-<短哈希>`。`latest` 始终指向 main 上最新的一次发布。

每个 release 同时附带 Linux 与 Windows 的免安装可执行文件，见[直接运行二进制](#直接运行二进制)。它们与容器镜像用同一份源码、同一次构建产出。

**主动指定递进哪一位**（比如改了大版本要发 `v1.0.0`）：

| 方式 | 用法 |
|---|---|
| 提交信息里写标记 | `[major]` / `[minor]` / `[patch]`，取提交信息里第一个出现的 |
| Actions 页面手动触发 | workflow_dispatch，选 patch / minor / major（它的优先级高于提交信息里的标记） |

```bash
# 例如把当前版本切成下一个大版本
git commit --allow-empty -m "Release v1.0.0 [major]"
git push
```

**不想为某次提交发版**：提交信息里写 `[skip release]`，测试照跑，只是不发版（它优先于 bump 标记）。

> 注意：`[major]` 这类标记只影响**版本号怎么算**，它不会替你改数据库迁移。改大版本时该动的迁移仍然要自己改，见下面「版本与数据迁移」。

部署时用发布出来的镜像即可，不需要在目标机器上编译：

```bash
docker pull ghcr.io/lyy1119/mycalendar:latest
docker run -d --name mycalendar -p 8080:8080 -v mycalendar-data:/data \
  ghcr.io/lyy1119/mycalendar:latest
```

在另一台机器上 pull 需要先具备该包的读取权限，否则会报 `unauthorized`；拿不到权限时本地构建即可（`make deploy` 会自动走这条路）。


用 compose 的话什么都不用改：模板默认就是这个镜像加 `pull_policy: missing`。想强制每次都拉最新，把 `missing` 改成 `always`。

只构建 amd64。要覆盖 Raspberry Pi 或 arm 的 NAS，在 workflow 的 `platforms:` 里加上 `linux/arm64` 即可——能work，只是 Go 编译会在模拟下跑，构建时间翻倍。

> 自动递增意味着**每次推 main 都会发一个版本**。对这个项目是合适的（一次提交一个可回溯的镜像）；若你更想要「只有显式打 tag 才发版」，把 workflow 里的 `on:` 改成 `push: tags: ['v*']`、并去掉自动算版本那一步即可。

### 构建时连不上网

`docker compose up -d --build` 时报这种错：

```
failed to solve: process "/bin/sh -c go mod download" did not complete successfully: exit code: 1
```

**这是网络问题，不是 Dockerfile 的问题。** 而且这行本身没用——真正的原因（DNS 解析失败、TLS 报错、连接超时）在它上面的几行里。先看完整输出：

```bash
docker compose build --no-cache --progress=plain 2>&1 | grep -B 20 "go mod download"
```

两种常见原因，症状一样：

**一、`proxy.golang.org` 在你的网络里不通**（国内最常见）。Go 默认的模块代理是 `proxy.golang.org`，被墙时会直接失败。改用镜像：

```bash
docker compose build --build-arg GOPROXY=https://goproxy.cn,direct
# 或直接 docker build：
docker build --build-arg GOPROXY=https://goproxy.cn,direct -t mycalendar .
```

模板里 `build.args.GOPROXY` 已经默认写成 `https://goproxy.cn,direct`，所以**从模板复制出来的 compose 不用额外做什么**；想回到官方代理就设 `GOPROXY=`。镜像代理同时代理校验和数据库（sumdb），所以 `GOSUMDB` 不用动。

**二、宿主机要走代理才能上网，但构建容器没有代理。** 这时宿主能通、容器不通，表现完全一样。把代理导出到 shell 即可，模板会转发：

```bash
export HTTPS_PROXY=http://10.0.0.1:8080
docker compose up -d --build
```

（`HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` 三个都已接到 Dockerfile 的构建参数上；它们只作用于构建阶段，**不会进最终镜像**——最终阶段是 scratch，不继承任何环境变量。）

先自查一下网络：

```bash
docker run --rm golang:1.27-alpine sh -c \
  "apk add --no-cache curl >/dev/null && curl -sS -o /dev/null -w '%{http_code}\n' https://proxy.golang.org/"
# 000 或超时 = 不通；200 = 通
```

**想彻底摆脱构建期网络**：在本地 `go mod vendor` 并把 `vendor/` 提交进仓库，再给 Dockerfile 的构建加上 `-mod=vendor`。代价是仓库里多出一份依赖源码，好处是构建完全离线。目前没这么做。

### 镜像内置的默认值

以下默认值都写在 Dockerfile 里，可用 `-e` 或 compose 的 `environment:` 覆盖：

| 变量 | 镜像默认值 | 说明 |
|---|---|---|
| `MYCAL_ADDR` | `:8080` | |
| `MYCAL_DB` | `/data/mycalendar.db` | `/data` 由服务在首次启动时自行创建，所以挂一个空卷即可 |
| `MYCAL_TZ` | `Asia/Shanghai` | **scratch 镜像里没有 `/etc/localtime`**，不显式指定就会是 UTC，所以这里给了一个默认值 |
| `MYCAL_WEEK_START` | `monday` | |
| `MYCAL_LOG_LEVEL` | `info` | |

### 数据库放到宿主机

镜像里数据库固定在 `/data/mycalendar.db`（`MYCAL_DB` 的镜像默认值）。要把它落到宿主机，映射的是**目录 `/data`**，不是那个 `.db` 文件：

```bash
# 方式一：命名卷（compose 默认）。数据在 /var/lib/docker/volumes/<项目名>_mycalendar-data 下，
# 由 Docker 管理，容器重建不丢，但平时不方便直接看到、拷贝。
docker run -d --name mycalendar -p 8080:8080 \
  -v mycalendar-data:/data mycalendar

# 方式二：绑定挂载到宿主机目录（想直接看到文件就用这个）
mkdir -p /srv/mycalendar
docker run -d --name mycalendar -p 8080:8080 \
  -v /srv/mycalendar:/data mycalendar
# 之后数据库就在 /srv/mycalendar/mycalendar.db

# SELinux 的宿主机（RHEL / Fedora / CentOS）要加 :Z，否则容器没有写权限
docker run -d --name mycalendar -p 8080:8080 \
  -v /srv/mycalendar:/data:Z mycalendar
```

用 compose 的话改你复制出来的 `docker-compose.yml` 里的 `volumes:` 即可，模板里两种写法都写好了，取消其中一行：

```yaml
    volumes:
      # - mycalendar-data:/data      # 命名卷（默认）
      - /srv/mycalendar:/data        # 绑定挂载
```

**为什么必须映射目录、不能只映射 `mycalendar.db` 这个文件**：SQLite 用的是 WAL 模式，会在数据库旁边额外维护 `mycalendar.db-wal` 与 `mycalendar.db-shm`。只把 `.db` 单文件挂进去，这两个伴生文件仍然写在容器内部，容器一重建就没了——**已经提交但还在 WAL 里的数据会一起丢**；数据库与其 WAL 分处不同文件系统本身也是隐患。所以宿主机目录里看到三个文件是正常的：

```
/srv/mycalendar/mycalendar.db
/srv/mycalendar/mycalendar.db-wal
/srv/mycalendar/mycalendar.db-shm
```

几点注意：

- 容器以 root 运行（`/data` 需要在首次启动时创建），所以宿主机上生成的这些文件属主是 `root:root`。要用普通用户读写，需自行 `chown`，或预先建好目录并放宽权限。
- 目录不存在时 Docker 会自动创建（属主 root），但建议先手动 `mkdir`，免得权限出乎意料。
- **备份要在容器停止后做，且三个文件一起拷。** 热拷贝一个正在写的 WAL 数据库可能拿到撕裂状态；想在线备份用 `sqlite3 mycalendar.db ".backup backup.db"`。
- 迁移到新机器：整个目录拷过去、路径保持一致即可。
- 想改容器内的文件名可以覆盖 `MYCAL_DB=/data/cal.db`，但通常没必要；改宿主机侧位置请改上面的 `-v`。

### `BUILD_VERSION` 是什么

`docker-compose.yml.template`（即你复制出来的 `docker-compose.yml`）里的 `VERSION: ${BUILD_VERSION:-compose}` 是一个**构建参数**，不是运行时配置：

```
docker-compose.yml 的 BUILD_VERSION
  → Dockerfile 的 ARG VERSION（默认 docker）
    → go build -ldflags "-X main.version=..."
      → 二进制里的 version 字符串
```

它只影响启动日志以及 `/healthz`、`/api`、`/help` 报告的版本号，用来确认线上跑的是哪一次构建。**程序本身从不读取这个名字的环境变量**——运行时真正会读的只有 `MYCAL_ADDR`、`MYCAL_DB`、`MYCAL_TZ`、`MYCAL_WEEK_START`、`MYCAL_CORS_ORIGINS`、`MYCAL_LOG_LEVEL`、`MYCAL_TRUST_PROXY` 这七个。

```bash
# 先确保已经复制过模板（见「用 compose 部署」）
BUILD_VERSION=1.2.3 docker compose up -d --build
curl -s http://127.0.0.1:8080/healthz   # version 字段即 1.2.3

# 不想每次传，也可以改 docker-compose.yml 里的 build.args.VERSION
```

不传则退回 `compose`（compose 构建）或 `docker`（直接 `docker build`）；本地 `make build` 是 `dev`。

> 这里原本叫 `MYCAL_VERSION`，看着和运行时那组 `MYCAL_*` 像一家人，实际是构建期变量，容易误以为要写进 `environment:`。已改名。

几个刻意的取舍：

- **`CGO_ENABLED=0`**：scratch 里没有 libc，动态链接的二进制会因为找不到 ELF 解释器而直接启动失败。纯 Go 的 SQLite 驱动让这一点成为可能。
- **`time/tzdata` 内嵌**：scratch 里没有 `/usr/share/zoneinfo`，具名时区（`Asia/Shanghai` 这类）全靠二进制内嵌的那份数据解析。
- **不声明 `VOLUME`**：那样每次 `docker run` 都会新建一个匿名卷，容器一重建日历就"消失"。改为要求在命令里显式 `-v`。
- **不在镜像里做健康检查**：scratch 没有 shell 也没有 curl，`HEALTHCHECK` 无从写起；请从宿主机探测 `curl -fsS http://127.0.0.1:8080/healthz`。
- **以 root 运行**：`/data` 需要在首次启动时创建，非 root 用户对根目录没有写权限。若你在意这一点，可预先创建好具名卷并改用 `--user`。

`.dockerignore` 会把 `node_modules`、构建产物、本地数据库和文档排除在构建上下文之外——数据库里是真实日程，绝不能被拷进镜像。

---

## 版本与数据迁移

### 版本号的含义

版本号是 `主.次.补丁`。补丁号由 CI 在每次推送到 main 时自动 +1（见「镜像发布（CI）」），主版本号手动决定。

**规则：只要改动了数据库格式，就必须升主版本号。** 反过来不成立——有时候只是改了 API 也会升主版本，那种情况下**不需要写迁移**。

### 升级前必读：只能逐个主版本升级

> ## ⚠️ 只能逐个大版本升级，不能跳版本
>
> 数据库只能从**上一个主版本**升级到当前主版本。例如现在要从 `v0.1.0` 升到 `v1.0.0`，直接升可以；但将来从 `v1.x` 跳到 `v3.x` **会被拒绝启动**，必须先跑一遍 `v2.x`。
>
> 原因：每一步迁移只在它紧邻的上一个版本上开发和测试过。跨版本串联执行等于把没测过的路径串起来跑，出问题就是数据问题。
>
> 被拒绝时启动会失败并明确告诉你该先跑哪个版本，**不会**动你的数据：
>
> ```
> mycalendar: database format 1 cannot be upgraded straight to 3: this project
> upgrades one major version at a time. Run the release for each intermediate
> major version, in order, before starting this one
> ```

### 迁移是怎么工作的

启动时自动进行，不需要手工执行任何命令：

1. 打开数据库，读 `meta` 表里的 `format_version`
2. 没有这个表或没有这个值 → 认为是很久以前（`v1.0.0` 之前）写的库，格式按 `0` 处理
3. 比当前版本旧 → 执行对应迁移，**每一步都在一个事务里完成，并同时更新版本号**；中途断电或被杀，下次启动会从原状态重来，不会留下半迁移的库
4. 等于当前版本 → 什么都不做（所以每次启动都会检查，重复启动是安全的）
5. 比当前版本新 → 拒绝启动，提示你升级服务端而不是降级

数据库格式版本与当前二进制的对应关系写在 `internal/store/migrate.go` 顶部的注释里。

### 改大版本时要动的地方

迁移是按版本号排好的模块化列表，就在 `internal/store/migrate.go`：

```go
// FormatVersion 必须与发布的主版本号保持一致
const FormatVersion = 1

// migrations[i] 把格式从 i 升到 i+1
var migrations = []step{
    {from: 0, to: 1, notes: "add events.location", apply: func(ctx, tx) error {
        _, err := tx.ExecContext(ctx, `ALTER TABLE events ADD COLUMN location TEXT`)
        return err
    }},
    // 下一个主版本写在这里。格式没变就留空 apply：
    // {from: 1, to: 2, notes: "no format change", apply: nil},
}
```

升主版本时：

1. 把 `FormatVersion` 改成新的主版本号
2. 在 `migrations` 末尾加一条 `{from: 旧, to: 新, ...}`
3. **格式真的变了** → 写 `apply`：注意 `ALTER TABLE` 只能往表尾追加列，所以 `currentSchema` 里也要把新列放在末尾，否则"全新库"和"迁移来的库"结构会不一致（有一条测试专门比对这两者，不一致会直接失败）
4. **格式没变**（只是改了 API 之类）→ `apply: nil`，并且 `notes` 要写清楚是"无格式变化"。留空是**刻意的**，不是忘了写

`apply: nil` 的步骤仍然会把版本号往前推，所以"某个主版本没有格式变化"这件事会被如实记录下来。

> 写迁移时请遵守：**永远不要修改或删除已经发布过的步骤**。那些步骤已经在别人的库里执行过了，改它只会让新旧库产生分歧。要修正就在后面追加新的步骤。

---

## 测试

```bash
make check                    # go vet + go test + 前端类型检查
make test-embed               # 构建前端后再测，额外校验打包产物确实被内嵌
make test-race                # 竞态检测
go test ./...                 # 后端全部测试
cd web && npm run typecheck   # 前端类型检查
cd web && npm run build       # 前端构建
```

后端的 `internal/webui` 测试用内存文件系统覆盖静态资源、SPA 兜底、缺失资源返回 404（而不是拿 HTML 顶替导致 MIME 报错）等行为；带 `webui_embedded` 标签时会额外校验真实打包产物存在且是生产版本。

测试使用临时目录中的独立数据库，不依赖外部服务，也不依赖宿主机时区（时区相关用例显式指定 `Asia/Shanghai` / `America/New_York` / `UTC` 等）。

覆盖重点：

- **时间窗口**：当地零点、夏令时 23/25 小时的一天、167 小时的周、跨年月份、闰年 2 月、`2026-02-30` 这类非法日期。
- **相交语义**：跨天日程、跨午夜日程、恰好结束于窗口起点的日程（不应命中）、无 `end_at` 的瞬时日程恰好落在窗口第一秒（应命中）。
- **全天日程的时区稳定性**：同一个全天日程在上海、UTC、纽约、Kiritimati、Midway 下的"当天"查询都能命中。
- **时区只影响解释**：同一瞬时在 UTC 与上海下落到不同日期，且数据未被改写。
- **三种时间写法等价**：Unix 秒、带偏移的 RFC3339、不带偏移的本地时间解析出同一瞬时；不带偏移的写法按请求 `tz` 解释，换个 `tz` 就换个瞬时。
- **时区回归**：浏览器端验证固定跑在 `Asia/Shanghai`（UTC 之前跑过一次，恰好掩盖了一个真实缺陷，见下）。
- **PATCH 语义**：缺省/置空/赋值三态、`{}` 空操作、全天与定时相互转换、`created_at` 不变而 `updated_at` 刷新。
- **校验**：标题与备注长度上限（按字符计，CJK 友好）、提醒数量上限与去重排序、时间戳越界与"毫秒当成秒"的识别提示、非法 `tz`/`range`/`date`/`from,to` 组合。
- **HTTP 层**：状态码与错误信封、未定义字段拒绝、带 `Allow` 头的 405、未知路径的 JSON 404、CORS 预检、`/help` 文档与实际路由及限额一致。
- **并发**：24 个 goroutine 并发写入不出现 `database is locked`。
- **前端路由**：`/` 归前端、未知 `/api/*` 仍返回 JSON 404、API 路径不会被 SPA 吞掉。
- **浏览器时区一致性**：日期表头显示的日子必须与列实际的日期一致；请求窗口必须是浏览器时区的零点；弹窗里手填的墙上时间必须原样画回网格。

### 浏览器端验证（可选）

`web/e2e/verify.mjs` 用 Playwright 打开真实页面并断言那些单元测试看不到的东西：4 天视图确实是 4 列、周视图确实是 7 列、全天日程**在滚动区之外且位于时间网格上方**、各天列共用同一个滚动区、点击日程弹出并预填编辑框、保存/完成任务/拖选新建/全天行新建/删除都真正落库、以及整轮没有 console 报错。用法见 `web/e2e/README.md`；它需要额外下载浏览器，**不参与常规构建**，且运行前会清空 ±90 天内的日程，请只对测试库使用。

---

## 项目结构

```
cmd/mycalendar/          程序入口：配置加载、优雅退出、HTTP 服务装配
web/                     前端源码（React + TypeScript + FullCalendar）
  ├── src/api.ts         API 客户端（时间一律 Unix 秒，附带浏览器时区）
  ├── src/eventMapping.ts API / FullCalendar / 编辑弹窗三方的字段互转
  ├── src/time.ts        时区与本地时间字符串工具
  ├── src/components/    日历面板、编辑弹窗、侧栏
  └── e2e/               可选的浏览器端验证
internal/webui/          内嵌并托管前端产物（webui_embedded 构建标签）
internal/api/            HTTP 层：路由、中间件、请求解析、错误映射
  ├── api.go             服务器装配、中间件（恢复/日志/CORS）、响应与错误信封
  ├── query.go           查询参数解析（tz、range/date、from/to、过滤、分页）
  ├── events.go          日程各接口的处理器
  └── help.go            /help 文档的数据结构与 Markdown 渲染
internal/service/        领域层：校验与规范化（"字段该如何理解"都在这里）
internal/store/          SQLite 持久化：增删改查、区间查询
  └── migrate.go         数据库格式版本与按主版本迁移的步骤表（改大版本时改这里）
internal/calendar/       时区相关的日历窗口计算（日/周/月/自定义）
internal/model/          与表结构一一对应的类型，以及 Optional / EventView
internal/config/         环境变量配置
internal/apperr/         错误分类（校验错误、参数错误、未找到）
```

数据表（与需求给定的一致；实现只额外加了索引）：

- `events`：`id, title, notes, start_at, end_at, is_all_day, event_date, is_task, is_completed, created_at, updated_at`
- `reminders`：`id, event_id, remind_at, created_at`

索引：`events(start_at)`、`events(end_at)`、`events(event_date)`、`events(is_all_day, is_task, is_completed)`、`reminders(event_id)`、`reminders(remind_at)`。

数据库格式版本记在 `meta` 表的 `format_version` 上（`v1.0.0` 之前用的是 `PRAGMA user_version`，现在那个值只是历史遗留，不再读取）。迁移在启动时自动进行，每一步一个事务、与版本号同时提交，因此中途中断只会重来而不会留下半迁移的库。重复启动幂等；若数据库来自更新的版本会拒绝启动。规则与改法见[版本与数据迁移](#版本与数据迁移)。

SQLite 连接使用 WAL 日志模式、`busy_timeout=5000`，并为每个连接启用外键约束（`foreign_keys` 是连接级设置，因此写在 DSN 里以确保连接池中每个连接都生效）——这也是删除日程能级联清理提醒的前提。

---

## 后续可做

- 提醒的实际调度与推送（当前只存不发）。
- WebSocket / SSE 推送变更，供客户端实时刷新。
- 重复日程（需要在表上增加 recurrence 规则字段）。
- 跨多天的全天事件（需要为全天引入结束日期字段）。
- 拖拽改期（当前刻意关闭，编辑统一走弹窗）。
- 提醒的调度与推送界面（后端已存 `remind_at`，前端暂未展示）。
