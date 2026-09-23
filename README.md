# KISS-WORKER

[English](README.en.md) | 简体中文

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/fishjar/kiss-worker)

适用于 [KISS-Translator](https://github.com/fishjar/kiss-translator) 的数据同步服务，可部署到 Cloudflare Workers，也可以使用 Docker 自托管。

服务端保持现有客户端协议兼容：`POST /sync` 用于同步设置、规则和生词本，`GET /rules?psk=...` 用于读取分享规则。Cloudflare 版本使用 Durable Object 保证同一数据键的并发写入顺序，并在首次访问时从旧 KV 自动迁移已有数据。

## 选择部署方式

Cloudflare 部署一共提供四种并列方式，建议按下表从上到下选择：

| 方式                                             | 适合用户               | 需要命令行 | Git 自动更新                      |
| ------------------------------------------------ | ---------------------- | ---------- | --------------------------------- |
| [一键部署](#方式一一键部署推荐)                  | 首次部署，希望步骤最少 | 否         | 是，Cloudflare 会创建一份仓库副本 |
| [GitHub 仓库导入](#方式二从-github-仓库导入)     | 希望维护自己的 Fork    | 否         | 是                                |
| [GitHub Actions](#方式三github-actions-自动部署) | 希望自行管理 CI/CD     | 否         | 是                                |
| [命令行部署](#方式四命令行部署)                  | 开发、调试或自定义配置 | 是         | 否                                |

四种 Cloudflare 方式都会创建或绑定所需的 KV 和 Durable Object，不需要手工创建 KV namespace，也不需要填写 namespace ID。“GitHub 仓库导入”由 Cloudflare Workers Builds 执行，“GitHub Actions”由 GitHub Runner 执行，两者是独立方案。Docker 自托管请参阅[独立章节](#docker-自托管)。

## 部署前准备

- 一个 [Cloudflare](https://dash.cloudflare.com/) 帐号。
- 一个足够长且不可预测的同步密码，部署时保存为 `AUTH_VALUE`。这是之后填写到 KISS-Translator 客户端的密码，不是 `CF_API_TOKEN`。
- 自定义域名为可选项；没有域名也可以使用 Cloudflare 提供的 `workers.dev` 地址。

## 方式一：一键部署（推荐）

适合新用户，无需下载源码或安装 Node.js。

1. 点击下面的按钮并登录 Cloudflare：

   [![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/fishjar/kiss-worker)

2. 按页面提示授权 GitHub、确认仓库副本名称和 Worker 名称。
3. 为 `AUTH_VALUE` 填写准备好的同步密码，然后开始部署。
4. 等待 Workers Builds 完成。Cloudflare 会根据 `wrangler.toml` 自动配置 KV 和 Durable Object。
5. 打开新建的 Worker，在概览或“Settings → Domains & Routes”中复制 `https://<worker-name>.<account-subdomain>.workers.dev` 地址。

一键按钮主要用于创建新部署。已有 Worker 请使用[仓库导入方式的原地升级说明](#现有-worker-原地升级)或命令行方式，以免误建一套空存储。

参考：[Cloudflare Deploy Button 官方文档](https://developers.cloudflare.com/workers/platform/deploy-buttons/)。

## 方式二：从 GitHub 仓库导入

这种方式不需要本地命令行，适合希望通过 Git 提交自动更新 Worker 的用户。

1. 在 GitHub 上 [Fork 本仓库](https://github.com/fishjar/kiss-worker/fork)。
2. 打开 Cloudflare Dashboard，进入“Workers & Pages → Create application → Import a repository”。
3. 连接 GitHub，选择刚刚创建的 Fork。
4. 确认构建配置：

   | 配置项            | 值               |
   | ----------------- | ---------------- |
   | Production branch | `master`         |
   | Root directory    | `/`              |
   | Build command     | `npm run build`  |
   | Deploy command    | `npm run deploy` |

5. 保存并部署。Cloudflare 会安装依赖，并自动配置 KV 和 Durable Object。
6. 初次部署后立即打开该 Worker 的“Settings → Variables & Secrets → Add”，添加名称为 `AUTH_VALUE`、类型为 **Secret** 的运行时变量，然后重新部署或重试失败的部署。

不要只把 `AUTH_VALUE` 添加到 Workers Builds 的构建变量中：构建变量只在构建阶段可见，Worker 运行时无法读取。设置完成后，推送到 Fork 的 `master` 分支会触发后续自动构建和部署。

参考：[Workers Builds](https://developers.cloudflare.com/workers/ci-cd/builds/)和[构建配置](https://developers.cloudflare.com/workers/ci-cd/builds/configuration/)。

## 方式三：GitHub Actions 自动部署

这种方式由 Fork 仓库中自带的 GitHub Actions 工作流执行测试、构建和部署，不需要在本地安装 Node.js。它与上一节的 Cloudflare Workers Builds 是并列方案，请勿同时用两套 CI 部署同一个 Worker，否则一次提交可能触发两次部署。

1. 在 GitHub 上 [Fork 本仓库](https://github.com/fishjar/kiss-worker/fork)。
2. 打开 Fork 的“Actions”页面并启用工作流。
3. 在 Cloudflare Dashboard 中复制目标帐号的 Account ID。
4. 在 Cloudflare 的“Account API Tokens”页面创建 API Token。使用 Workers 编辑权限，并将帐号和区域资源限制到实际部署范围。
5. 打开 Fork 的“Settings → Secrets and variables → Actions”，添加两个 Repository secret：

   | Secret          | 内容                              |
   | --------------- | --------------------------------- |
   | `CF_ACCOUNT_ID` | Cloudflare Account ID             |
   | `CF_API_TOKEN`  | 上一步创建的 Cloudflare API Token |

6. 打开“Actions → Cloudflare Worker → Run workflow”，选择 `master` 后执行首次部署。单纯 Fork 仓库不会产生一次新的 push 事件，因此第一次需要手动触发。
7. 部署完成后，打开 Cloudflare Worker 的“Settings → Variables & Secrets → Add”，添加名称为 `AUTH_VALUE`、类型为 **Secret** 的运行时变量，然后重新部署。
8. 后续推送到 Fork 的 `master` 分支时，GitHub Actions 会自动执行 `npm ci`、测试、dry-run 构建和正式部署。

`CF_ACCOUNT_ID` 和 `CF_API_TOKEN` 只用于 GitHub Actions 向 Cloudflare 部署代码；`AUTH_VALUE` 是 KISS-Translator 客户端使用的同步密码。三者用途不同，不能混用。缺少任意一个 GitHub Secret 时，工作流仍会运行测试和构建，但会安全跳过 Deploy 步骤。

参考：[Cloudflare GitHub Actions 官方文档](https://developers.cloudflare.com/workers/ci-cd/external-cicd/github-actions/)。

## 方式四：命令行部署

### 前提条件

- [Git](https://git-scm.com/)
- Node.js 22 或更高版本

### 部署步骤

```sh
git clone https://github.com/fishjar/kiss-worker.git
cd kiss-worker
npm ci
npx wrangler login
npm run deploy
npm run secret
```

首次运行 `npx wrangler login` 会打开 Cloudflare 授权页面。`npm run deploy` 自动配置 KV 和 Durable Object；`npm run secret` 会提示输入 `AUTH_VALUE`，输入内容不会写入仓库。Secret 设置成功后，Wrangler 会为 Worker 创建新版本。

本地调试时，可将 `.dev.vars.example` 复制为 `.dev.vars`，替换其中的示例密码，然后运行：

```sh
npm start
```

`.dev.vars` 已被 Git 忽略，不要把真实密码提交到仓库。

## 部署后配置

### 配置 KISS-Translator

1. 在 Cloudflare Worker 页面复制 `workers.dev` 地址；如果配置了自定义域名，也可以使用自定义地址。
2. 在 KISS-Translator 的同步设置中填写该地址。
3. 同步密钥填写部署时设置的 `AUTH_VALUE` 原始值，而不是哈希值或 Cloudflare API Token。
4. 分别同步设置、规则和生词本进行验证；如使用规则分享，再验证已有分享链接。

服务端公开接口保持为 `POST /sync` 和 `GET /rules?psk=...`，已有客户端无需修改接口、字段或协议。

### 配置自定义域名

进入 Worker 的“Settings → Domains & Routes → Add → Custom Domain”，选择 Cloudflare 帐号中管理的域名。设置完成后，将客户端同步地址改为该 HTTPS 地址。

### 现有 Worker 原地升级

现有用户无需创建新 Worker，也无需重新配置客户端：

- 仓库方式：打开现有 Worker 的“Settings → Builds → Connect”，连接本仓库或自己的 Fork。确保 Dashboard 中的 Worker 名称与 `wrangler.toml` 的 `name` 一致。
- GitHub Actions 方式：确保 `wrangler.toml` 中的名称仍指向现有 Worker，并在 Fork 中配置 `CF_ACCOUNT_ID` 与 `CF_API_TOKEN` 后手动触发工作流。
- 命令行方式：保留现有 Worker 名称和 `KV` binding，执行 `npm ci`、`npm run deploy` 和 `npm run secret`。

部署 Durable Object 版本前建议备份 KV。旧 KV 记录会在对应 key 首次访问时原样迁移，迁移完成后 Durable Object 是新数据的权威来源。新写入不会回写旧 KV，因此回滚到不支持 Durable Object 的旧代码前，应先导出升级后新增的数据。

## 故障排查

| 现象                                        | 检查方法                                                                                     |
| ------------------------------------------- | -------------------------------------------------------------------------------------------- |
| 返回 `503 Must set AUTH_VALUE environment.` | 在“Settings → Variables & Secrets”中添加运行时 Secret `AUTH_VALUE`，然后重新部署             |
| `/sync` 或 `/rules` 返回 `403`              | 确认客户端使用的是 `AUTH_VALUE` 原始值，并检查地址是否指向正确的 Worker                      |
| GitHub 导入构建失败                         | 查看 Workers Builds 日志，确认生产分支、根目录和构建/部署命令与本文一致                      |
| GitHub Actions 成功但 Deploy 被跳过         | 确认 Fork 同时配置了 `CF_ACCOUNT_ID` 和 `CF_API_TOKEN` 两个 Repository secret                |
| GitHub Actions 中 Wrangler 认证失败         | 检查 Account ID、API Token 权限及 Token 的帐号资源范围                                       |
| 更新后部署到了另一个 Worker                 | 确认 Dashboard Worker 名称与 `wrangler.toml` 中的 `name = "kiss-worker"` 一致                |
| 一次提交产生两次部署                        | 不要让 Workers Builds 和 GitHub Actions 同时部署同一个 Worker，保留其中一种 Git 自动部署方式 |

## 安全说明

- `AUTH_VALUE` 应使用独立的高强度随机字符串，不要复用 Cloudflare、GitHub 或其他帐号密码。
- 不要提交 `.dev.vars`、`.env`、真实 Secret 或 API Token。
- `AUTH_VALUE` 是 Worker 运行时 Secret；`CF_API_TOKEN` 和 `CF_ACCOUNT_ID` 仅供 GitHub Actions 部署，两类凭据不可互换。
- 始终使用 HTTPS。`/rules?psk=...` 中的 `psk` 可访问分享规则，应像凭据一样保护分享链接，避免出现在公开日志或页面中。

## Docker 自托管

Docker 是 Cloudflare 之外的独立部署方式，适合拥有服务器并希望自行管理数据的用户。

### 前提条件

- 安装 Docker 和 Docker Compose。
- 服务器端口可访问，生产环境建议通过反向代理提供 HTTPS。

### 启动

```sh
git clone https://github.com/fishjar/kiss-worker.git
cd kiss-worker
```

在项目目录创建 `.env`，设置服务端必需的 `APP_KEY`。它对应客户端同步密钥，作用与 Cloudflare 部署的 `AUTH_VALUE` 相同：

```env
APP_KEY=replace-with-a-long-random-secret
```

启动并查看日志：

```sh
docker compose up -d
docker compose logs -f kiss-worker
```

默认映射到宿主机端口 `8080`，同步地址为 `http://<服务器地址>:8080`。生产环境应配置 HTTPS。数据保存在项目目录的 `data/` 中，升级或重建容器时不要删除该目录。

### 升级

```sh
git pull
docker compose pull
docker compose up -d
```

升级前建议备份 `.env` 和 `data/`。如果使用本地源码构建镜像，请取消 `docker-compose.yml` 中的 `build: .` 注释，并按自己的镜像管理流程升级。

## 开发与验证

```sh
npm ci
npm test
npm run build
```

Go/Docker 后端可执行：

```sh
go test ./...
go vet ./...
```

## License

[LICENSE](LICENSE)
