# ECS Controller

ECS Controller 是一个用于阿里云 ECS 的轻量控制台，主要做三件事：

- 查看账号 CDT 月流量和单台实例参考流量；
- 自动发现账号下的地域和 ECS 实例；
- 对符合条件的实例做后台保活，并提供 Web 手工启动、关机、设置和日志查看。

项目使用 Go 编写，前端是内置静态文件，支持 Docker Compose 部署。容器内固定使用 `/app` 放程序和 Web 文件，使用 `/data` 保存运行时设置和状态。

## 功能

- 支持多个阿里云账号。
- 支持中国站账号和国际站账号。
- 支持自动发现地域，也支持手工指定地域。
- 支持 IPv4 和 IPv6 展示。
- 支持账号级 CDT 流量查看，按中国内地和非中国内地拆分额度池。
- 支持实例级 CMS 本月流量估算。
- 支持保活所有实例、只保活抢占式实例、只保活指定实例或关闭保活。
- 支持流量超阈值后继续保活、暂停保活或转人工决策。
- 支持 Web 手工启动、手工关机。
- 支持全局停机模式，非包年包月实例可使用节省停机，包年包月实例自动降级为普通停机。
- 支持手工关机后暂停该实例后台保活。
- 支持企业微信自建应用文本通知。
- 支持 Web 设置页修改非密钥类运行参数。
- 支持运行日志页面，方便查看巡检、保活、流量和通知事件。

## 快速开始

复制环境变量示例：

```bash
cp .env.example .env
```

编辑 `.env`，至少修改这些变量：

```env
EC_PASSWORD=change-me-to-a-long-random-password
EC_ACCOUNTS=CN1
EC_ACCOUNT_CN1_NAME=aliyun-cn-1
EC_ACCOUNT_CN1_SITE=china
EC_ACCOUNT_CN1_ACCESS_KEY_ID=your-access-key-id
EC_ACCOUNT_CN1_ACCESS_KEY_SECRET=your-access-key-secret
EC_ACCOUNT_CN1_REGIONS=auto
```

启动：

```bash
docker compose up -d --build
```

访问：

```text
http://你的服务器IP:43210
```

登录密码是 `.env` 里的 `EC_PASSWORD`。它只用于保护这个控制台，不是阿里云账号密码，也不是 AccessKey。

## 目录结构

```text
.
├── cmd/ecs-controller/        # 程序入口
├── internal/aliyun/           # 阿里云 ECS、CMS、CDT API 调用
├── internal/applog/           # 应用日志
├── internal/config/           # 环境变量、运行时设置读取与写回
├── internal/monitor/          # 巡检、保活、状态缓存、通知触发
├── internal/notify/           # 企业微信通知
├── internal/web/              # Web API 和静态文件服务
├── web/                       # 前端静态文件
├── Dockerfile
├── docker-compose.yml
├── .env.example
└── README.md
```

容器内路径固定如下：

```text
/app/ecs-controller      # Go 主程序
/app/web                 # Web 静态文件
/data/settings.yaml      # Web 设置页写回的运行时配置
/data/state.json         # 手工暂停、最近操作、实例流量缓存等状态
```

默认 Docker Compose 使用命名卷 `ecs-controller-data:/data` 持久化运行数据。

## 配置来源

程序启动时读取固定路径：

```text
/data/settings.yaml
```

如果这个文件不存在，程序会使用默认值和环境变量生成运行配置。Web 设置页保存后，会把非密钥类配置写入 `/data/settings.yaml`。

这些内容仍然只从环境变量读取，不会通过 Web 页面展示或保存：

- 阿里云 AccessKey ID；
- 阿里云 AccessKey Secret；
- 阿里云账号列表；
- 企业微信 `corpid`；
- 企业微信 `corpsecret`；
- 企业微信 `agentid`；
- 企业微信接收人。

`/data/state.json` 只保存运行状态，不是配置文件。它包含：

- 手工关机后暂停保活的实例 ID；
- 最近启动时间；
- 最近操作记录；
- 实例本月流量缓存。

实例停机后，CMS 某些公网 IP 维度可能临时读不到历史点。程序会使用 `/data/state.json` 里的本月最大流量缓存，避免页面流量从已有值回退成 `0.00GB`。

## 环境变量总览

### 服务参数

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `EC_LISTEN` | 否 | `:8080` | 容器内 HTTP 监听地址。一般保持默认即可，由 Compose 映射到宿主机端口。 |
| `EC_PASSWORD` | 是 | 无 | Web 登录密码。建议使用长随机字符串。 |
| `EC_REFRESH_INTERVAL` | 否 | `5m` | 后台主巡检间隔。会刷新账号流量、实例列表、实例流量和保活决策。 |
| `EC_REQUEST_TIMEOUT` | 否 | `20s` | 单次阿里云 API 请求超时。 |
| `EC_REGION_REFRESH_INTERVAL` | 否 | `24h` | `regions=auto` 时地域列表缓存时间，避免每轮都调用 `DescribeRegions`。 |
| `EC_MAX_CONCURRENCY` | 否 | `4` | 预留并发上限。当前主要作为配置项保留。 |
| `EC_LOG_LEVEL` | 否 | `info` | 日志级别，可填 `debug`、`info`、`warn`、`error`。 |

时间格式使用 Go duration，例如：

```text
30s
5m
1h
24h
```

### 流量参数

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `EC_TRAFFIC_WARNING_PERCENT` | 否 | `95` | 流量告警阈值百分比。例如 `95` 表示达到额度的 95% 后告警或触发对应保活策略。 |

账号流量按两个额度池计算：

- 中国内地：默认建议 `20GB`；
- 非中国内地：默认建议 `200GB`。

额度池是账号级共享的，不是单台实例独占的。同一账号下多个实例在同一个分区内会共享对应额度。

### 保活参数

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `EC_KEEP_ALIVE_ENABLED` | 否 | `true` | 是否启用后台自动保活。关闭后仍可查看和手工操作。 |
| `EC_KEEP_ALIVE_TARGET` | 否 | `spot_only` | 保活目标，支持 `disabled`、`all`、`spot_only`、`include_list`。 |
| `EC_TRAFFIC_POLICY` | 否 | `manual_only_when_exceeded` | 流量策略，支持 `ignore_limit`、`pause_when_exceeded`、`manual_only_when_exceeded`。 |
| `EC_START_COOLDOWN` | 否 | `10m` | 同一实例重复启动保护间隔，避免短时间重复调用 `StartInstance`。 |
| `EC_STOP_MODE` | 否 | `StopCharging` | 默认停机模式，支持 `StopCharging` 和 `KeepCharging`。 |
| `EC_INCLUDE_INSTANCE_IDS` | 否 | 空 | 指定保活实例 ID。仅在 `EC_KEEP_ALIVE_TARGET=include_list` 时使用，多个用逗号分隔。 |

`EC_KEEP_ALIVE_TARGET` 可选值：

| 值 | 说明 |
| --- | --- |
| `disabled` | 关闭后台保活，只保留查看和手工操作。 |
| `all` | 所有发现到的实例都参与保活。 |
| `spot_only` | 只保活抢占式实例。默认推荐。 |
| `include_list` | 只保活 `EC_INCLUDE_INSTANCE_IDS` 里列出的实例。 |

`EC_TRAFFIC_POLICY` 可选值：

| 值 | 说明 |
| --- | --- |
| `ignore_limit` | 忽略流量限制继续保活。 |
| `pause_when_exceeded` | 实例所属流量额度池超过阈值后暂停后台自动保活。 |
| `manual_only_when_exceeded` | 未超过阈值时自动保活；超过阈值后后台不自动保活，但页面仍允许手工启动。 |

`EC_STOP_MODE` 可选值：

| 值 | 说明 |
| --- | --- |
| `StopCharging` | 节省停机。非包年包月实例可使用；包年包月实例会自动降级为 `KeepCharging`。 |
| `KeepCharging` | 普通停机。关机请求保持普通停机模式。 |

手工关机成功后，该实例会被写入手工暂停列表，下一轮后台保活不会立刻把它重新拉起。页面手工启动成功后会解除这个暂停状态。

### 企业微信通知参数

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `EC_NOTIFY_ENABLED` | 否 | `false` | 是否启用企业微信通知。 |
| `EC_WECHAT_CORPID` | 启用通知时必填 | 空 | 企业微信企业 ID。 |
| `EC_WECHAT_CORPSECRET` | 启用通知时必填 | 空 | 企业微信自建应用 Secret。 |
| `EC_WECHAT_AGENTID` | 启用通知时必填 | `0` | 企业微信自建应用 AgentId。 |
| `EC_WECHAT_TOUSER` | 启用通知时必填 | 空 | 接收人。单人写 `user1`，多人写 `user1,user2`。 |
| `EC_NOTIFY_EVENTS` | 否 | `auto_start`<br>`manual_start`<br>`manual_stop`<br>`manual_required`<br>`traffic_exceeded`<br>`error` | 通知事件列表，多个用逗号分隔。 |

通知使用企业微信自建应用文本消息，不使用群机器人 webhook。

`EC_NOTIFY_EVENTS` 可选值：

| 值 | 说明 |
| --- | --- |
| `auto_start` | 后台自动提交启动。 |
| `manual_start` | 页面手工启动。 |
| `manual_stop` | 页面手工关机。 |
| `manual_required` | 流量超阈值或流量未知，需要人工决策。 |
| `traffic_exceeded` | 账号某个流量额度池达到告警阈值。 |
| `error` | 阿里云接口、启动、关机或通知发送失败。 |
| `all` | 发送全部事件。 |

实例相关通知示例：

```text
账号：Huhu
事件：手工关机
停机模式：节约关机
地域：cn-hangzhou
实例名称：example
实例 ID：i-xxxxxxxx
发送时间：2026-05-19 05:06:05
```

### 多账号参数

`EC_ACCOUNTS` 是账号别名列表：

```env
EC_ACCOUNTS=CN1,CN2,INTL1
```

每个别名会转换成大写下划线形式，拼到变量名里：

```text
EC_ACCOUNT_<别名>_NAME
EC_ACCOUNT_<别名>_SITE
EC_ACCOUNT_<别名>_ACCESS_KEY_ID
EC_ACCOUNT_<别名>_ACCESS_KEY_SECRET
EC_ACCOUNT_<别名>_REGIONS
EC_ACCOUNT_<别名>_MAINLAND_TRAFFIC_LIMIT
EC_ACCOUNT_<别名>_OVERSEAS_TRAFFIC_LIMIT
```

例如别名 `CN1`：

```env
EC_ACCOUNT_CN1_NAME=aliyun-cn-1
EC_ACCOUNT_CN1_SITE=china
EC_ACCOUNT_CN1_ACCESS_KEY_ID=your-cn1-ak
EC_ACCOUNT_CN1_ACCESS_KEY_SECRET=your-cn1-sk
EC_ACCOUNT_CN1_REGIONS=auto
EC_ACCOUNT_CN1_MAINLAND_TRAFFIC_LIMIT=20
EC_ACCOUNT_CN1_OVERSEAS_TRAFFIC_LIMIT=200
```

例如别名 `intl-prod` 会被转换成 `INTL_PROD`，对应变量为：

```env
EC_ACCOUNT_INTL_PROD_NAME=aliyun-intl-prod
EC_ACCOUNT_INTL_PROD_SITE=international
EC_ACCOUNT_INTL_PROD_ACCESS_KEY_ID=your-intl-ak
EC_ACCOUNT_INTL_PROD_ACCESS_KEY_SECRET=your-intl-sk
EC_ACCOUNT_INTL_PROD_REGIONS=auto
EC_ACCOUNT_INTL_PROD_MAINLAND_TRAFFIC_LIMIT=20
EC_ACCOUNT_INTL_PROD_OVERSEAS_TRAFFIC_LIMIT=200
```

账号变量说明：

| 变量后缀 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `NAME` | 否 | 账号别名 | Web 页面展示名称。 |
| `SITE` | 是 | `china` | 阿里云站点，支持 `china` 和 `international`。 |
| `ACCESS_KEY_ID` | 是 | 无 | 阿里云 AccessKey ID。 |
| `ACCESS_KEY_SECRET` | 是 | 无 | 阿里云 AccessKey Secret。 |
| `REGIONS` | 否 | `auto` | 地域列表。`auto` 表示自动探测；也可以写 `cn-hangzhou,cn-shanghai`。 |
| `MAINLAND_TRAFFIC_LIMIT` | 否 | `20` | 中国内地 CDT 月额度，单位 GB。 |
| `OVERSEAS_TRAFFIC_LIMIT` | 否 | `200` | 非中国内地 CDT 月额度，单位 GB。 |

可以同时配置多个中国站账号，也可以同时配置多个国际站账号：

```env
EC_ACCOUNTS=CN1,CN2,INTL1,INTL2
```

然后分别补齐每个账号的 `EC_ACCOUNT_<别名>_*` 变量即可。

## 完整 .env 示例

```env
EC_LISTEN=:8080
EC_REFRESH_INTERVAL=5m
EC_REQUEST_TIMEOUT=20s
EC_PASSWORD=change-me-to-a-long-random-password
EC_REGION_REFRESH_INTERVAL=24h
EC_MAX_CONCURRENCY=4
EC_TRAFFIC_WARNING_PERCENT=95
EC_LOG_LEVEL=info

EC_NOTIFY_ENABLED=false
EC_WECHAT_CORPID=
EC_WECHAT_CORPSECRET=
EC_WECHAT_AGENTID=0
EC_WECHAT_TOUSER=
EC_NOTIFY_EVENTS=auto_start,manual_start,manual_stop,manual_required,traffic_exceeded,error

EC_KEEP_ALIVE_ENABLED=true
EC_KEEP_ALIVE_TARGET=spot_only
EC_TRAFFIC_POLICY=manual_only_when_exceeded
EC_START_COOLDOWN=10m
EC_STOP_MODE=StopCharging
EC_INCLUDE_INSTANCE_IDS=

EC_ACCOUNTS=CN1,INTL1

EC_ACCOUNT_CN1_NAME=aliyun-cn-1
EC_ACCOUNT_CN1_SITE=china
EC_ACCOUNT_CN1_ACCESS_KEY_ID=your-china-site-ak
EC_ACCOUNT_CN1_ACCESS_KEY_SECRET=your-china-site-sk
EC_ACCOUNT_CN1_REGIONS=auto
EC_ACCOUNT_CN1_MAINLAND_TRAFFIC_LIMIT=20
EC_ACCOUNT_CN1_OVERSEAS_TRAFFIC_LIMIT=200

EC_ACCOUNT_INTL1_NAME=aliyun-intl-1
EC_ACCOUNT_INTL1_SITE=international
EC_ACCOUNT_INTL1_ACCESS_KEY_ID=your-international-site-ak
EC_ACCOUNT_INTL1_ACCESS_KEY_SECRET=your-international-site-sk
EC_ACCOUNT_INTL1_REGIONS=auto
EC_ACCOUNT_INTL1_MAINLAND_TRAFFIC_LIMIT=20
EC_ACCOUNT_INTL1_OVERSEAS_TRAFFIC_LIMIT=200
```

## Web 设置页能改什么

Web 设置页只能修改非密钥项，包括：

- 后台检查间隔；
- 地域缓存时间；
- API 请求超时；
- 流量告警阈值；
- 是否启用保活；
- 保活目标；
- 流量策略；
- 重复启动保护间隔；
- 停机模式；
- 指定保活实例 ID；
- 日志级别；
- 是否启用通知；
- 通知事件。

保存后会写入：

```text
/data/settings.yaml
```

这些内容不会通过 Web 修改：

- `EC_ACCOUNTS`；
- `EC_ACCOUNT_<别名>_ACCESS_KEY_ID`；
- `EC_ACCOUNT_<别名>_ACCESS_KEY_SECRET`；
- `EC_ACCOUNT_<别名>_SITE`；
- `EC_ACCOUNT_<别名>_REGIONS`；
- `EC_WECHAT_CORPID`；
- `EC_WECHAT_CORPSECRET`；
- `EC_WECHAT_AGENTID`；
- `EC_WECHAT_TOUSER`。

如果要修改账号、地域、密钥或企业微信应用凭据，请编辑 `.env` 并重启容器。

## 阿里云 RAM 权限要求

建议为本项目单独创建 RAM 用户或 RAM 角色，并只授予必要权限。当前测试可用的权限模板如下：

```json
{
  "Version": "1",
  "Statement": [
    {
      "Resource": "*",
      "Effect": "Allow",
      "Action": [
        "ecs:StopInstance",
        "ecs:StartInstance",
        "ecs:DescribeRegions",
        "ecs:DescribeInstances",
        "ecs:DescribeInstanceStatus",
        "ecs:DescribeNetworkInterfaces"
      ]
    },
    {
      "Resource": "*",
      "Effect": "Allow",
      "Action": "cms:QueryMetricList"
    },
    {
      "Resource": "*",
      "Effect": "Allow",
      "Action": "cdt:ListCdtInternetTraffic"
    }
  ]
}
```

权限用途：

| 权限 | 用途 |
| --- | --- |
| `ecs:DescribeRegions` | 自动发现账号可用地域。 |
| `ecs:DescribeInstances` | 读取 ECS 实例、规格、状态、公网 IP、计费方式等信息。 |
| `ecs:DescribeInstanceStatus` | 预留给实例状态查询。 |
| `ecs:DescribeNetworkInterfaces` | 读取网卡 IPv6 地址。 |
| `ecs:StartInstance` | 后台保活和页面手工启动。 |
| `ecs:StopInstance` | 页面手工关机。 |
| `cms:QueryMetricList` | 读取云监控指标，用于估算实例本月流量。 |
| `cdt:ListCdtInternetTraffic` | 读取账号 CDT 流量，用于账号级流量额度和保活阈值判断。 |

代码调用的云监控 API 名称是 `DescribeMetricList`，RAM 授权 Action 使用的是 `cms:QueryMetricList`。这是阿里云云监控接口名和 RAM 权限名不完全一致导致的，授权时按上面的 `cms:QueryMetricList` 配置。

如果实例流量读取失败，可以临时给 RAM 用户增加云监控只读类系统策略做排查。确认问题后建议收回临时权限，只保留上面的最小权限。

## 流量口径

### 账号流量

账号流量来自 CDT：

```text
cdt:ListCdtInternetTraffic
```

程序会根据 CDT 返回的 `BusinessRegionId` 把流量拆成两个分区：

- 中国内地：`cn-` 开头且不是 `cn-hongkong` 的地域；
- 非中国内地：`cn-hongkong`、日本、新加坡、美国、欧洲等其他地域。

保活策略按实例所在地域选择对应分区判断阈值。例如：

- `cn-hangzhou` 使用中国内地额度；
- `cn-hongkong` 使用非中国内地额度；
- `ap-northeast-1` 使用非中国内地额度。

### 实例流量

实例流量来自云监控 CMS：

```text
Namespace: acs_ecs_dashboard
MetricName: VPC_PublicIP_InternetOutRate 或 InternetOutRate
```

实例流量是按云监控速率点估算出来的本月公网出方向流量，适合看单台实例趋势。最终是否暂停保活，仍以账号级 CDT 分区流量为准。

## 日志

Docker Compose 默认设置：

```yaml
environment:
  TZ: Asia/Shanghai
```

镜像内已安装 `tzdata`，程序会使用 `TZ` 设置 Go 本地时区。应用日志会带本地时间和级别，例如：

```text
2026-05-19 05:36:28 [INFO] refresh finished accounts=1 duration=16.5s errors=0 instances=3
2026-05-19 05:36:28 [INFO] keepalive check finished checked=3 manual_required=0 skipped=3 starts=0
2026-05-19 05:36:18 [DEBUG] traffic cms instance traffic loaded account=Huhu instance=i-xxx region=cn-hangzhou used=0.11GB
```

Web 控制台的“运行日志”页读取应用内存中的最近日志，不读取宿主机 Docker daemon 日志。

如果使用 `docker logs -t`，Docker 自己加上的时间戳可能是 Docker daemon 的时区；看应用日志行里的时间前缀即可。

## Web 静态文件

Web 文件位于：

```text
web/index.html
web/assets/styles.css
web/assets/app.js
```

Dockerfile 会把 `web/` 打包进镜像。只修改页面文案或样式时，通常只需要改 `web/` 目录并重新构建镜像，不需要改 Go 源码。

## Docker Compose

默认 `docker-compose.yml`：

```yaml
services:
  ecs-controller:
    build: .
    container_name: ecs-controller
    restart: unless-stopped
    ports:
      - "43210:8080"
    env_file:
      - .env
    environment:
      TZ: Asia/Shanghai
    volumes:
      - ecs-controller-data:/data
    dns:
      - 223.5.5.5
      - 114.114.114.114

volumes:
  ecs-controller-data:
```

常用命令：

```bash
docker compose up -d --build
docker compose logs -f
docker compose restart
docker compose down
```

保留数据并重建：

```bash
docker compose up -d --build
```

删除容器但保留命名卷：

```bash
docker compose down
```

如果执行 `docker compose down -v`，会删除 `/data` 对应的命名卷，Web 设置、手工暂停状态和实例流量缓存都会丢失。

## 本地开发

运行测试：

```bash
go test ./...
```

构建：

```bash
go build -o ecs-controller ./cmd/ecs-controller
```

本地直接运行时，程序固定读取 `/data/settings.yaml` 并使用 `/app/web`。更推荐用 Docker Compose 调试。

## 镜像和版本

仓库包含 GitHub Actions 工作流，会在推送到 `main` 或推送 `v*.*.*` 标签时自动构建 Docker 镜像。

默认镜像地址：

```text
ghcr.io/sunser/ecs-controller
```

镜像使用 Docker Buildx 构建，当前发布以下架构：

| 平台 | 常见设备 |
| --- | --- |
| `linux/amd64` | 常见 x86_64 云服务器、PC、NAS |
| `linux/arm64` | ARM64 云服务器、Apple Silicon、部分 NAS 和开发板 |
| `linux/arm/v7` | 32 位 ARMv7 设备 |

使用同一个镜像 tag 即可，Docker 会按当前机器架构自动选择对应镜像。例如：

```bash
docker pull ghcr.io/sunser/ecs-controller:latest
```

如果需要在本地手工构建多架构镜像，可以使用：

```bash
docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 -t ghcr.io/sunser/ecs-controller:local .
```

推送到 `main` 分支后会生成：

```text
ghcr.io/sunser/ecs-controller:latest
ghcr.io/sunser/ecs-controller:main
ghcr.io/sunser/ecs-controller:sha-<commit>
```

发布版本时，先更新 `VERSION` 文件，例如：

```text
0.1.0
```

然后创建并推送对应 tag：

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions 会额外生成版本镜像：

```text
ghcr.io/sunser/ecs-controller:0.1.0
ghcr.io/sunser/ecs-controller:0.1
```

拉取镜像：

```bash
docker pull ghcr.io/sunser/ecs-controller:latest
```

如果要直接使用远程镜像部署，可以把 `docker-compose.yml` 里的：

```yaml
build: .
```

改成：

```yaml
image: ghcr.io/sunser/ecs-controller:latest
```
