# CloudFlare Tools

一个功能强大的 CloudFlare 批量管理工具,支持域名、DNS、SSL、规则、缓存等多种批量操作。

## 功能特性

### 域名解析管理
- **批量添加域名** - 快速将多个域名添加到 CloudFlare
- **批量解析记录** - 支持每个域名解析到不同的 IP
- **批量删除解析** - 清空或删除指定解析记录
- **批量开关代理** - 一键开启/关闭 CDN 代理
- **批量删除域名** - 批量移除 Zone
- **批量导出域名** - 导出域名列表及状态

### 安全规则
- **SSL/HTTPS 设置** - 批量配置 SSL 模式、TLS 版本、HTTPS 重定向
- **证书申请** - 一键申请 Let's Encrypt 免费 SSL 证书，支持通配符域名
- **批量复制规则** - 复制页面规则、防火墙规则、速率限制
- **批量删除规则** - 清空各类规则配置

### 高级设置
- **缓存管理** - 批量清除缓存、设置缓存级别、Always Online
- **性能优化** - 代码压缩、Brotli、HTTP/2、HTTP/3、图像优化
- **批量配置** - 安全级别、浏览器检查、防盗链等批量设置

### 账号管理
- 多账号管理
- 账号连接测试
- 搜索和筛选
- 批量检测和删除
- 状态监控(正常/失效)

## 技术栈

- **后端**: Go + Gin
- **前端**: 原生 JavaScript + Vite + Tabler UI
- **部署**: 单文件二进制(内嵌前端资源)

## 快速开始

### 方式一：Docker 部署（推荐）

#### 使用 GHCR 预构建镜像

推送到 `main` 分支发布 `edge`，不会覆盖稳定版。推送稳定 Git 标签 `v0.01` 时会同时发布 `0.01`、`v0.01` 和 `latest`；也可以在 GitHub Actions 中手动填写版本，手动发布默认不更新 `latest`。

```bash
docker pull ghcr.io/willamblack/cloudflare-tools:0.01
```

准备配置和持久化目录（`accounts.json` 和证书也会保存在这个目录）：

```bash
umask 077
mkdir -p data/certs
cp Server/config.yaml.example data/config.yaml
chmod 700 data data/certs
chmod 600 data/config.yaml
```

修改 `data/config.yaml` 中的管理员用户名和密码后启动：

```bash
docker run -d \
  --name cloudflare-tools \
  --restart unless-stopped \
  -p 127.0.0.1:28080:8080 \
  -e TZ=Asia/Shanghai \
  -e DATA_DIR=/data \
  -e JWT_SECRET="$(openssl rand -hex 32)" \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --read-only \
  --tmpfs /tmp:size=64m,mode=1777 \
  -v "$(pwd)/data:/data" \
  -v cloudflare-tools-acme:/root/.acme.sh \
  ghcr.io/willamblack/cloudflare-tools:0.01
```

也可以不挂载配置文件，改用 `ADMIN_USERNAME`、`ADMIN_PASSWORD` 环境变量，但环境变量可被有 Docker 管理权限的人通过容器配置查看，不等于加密存储。`JWT_SECRET` 至少 32 个字符；未设置时程序会随机生成，但重启会使已有登录失效。GHCR 镜像 `0.01` 已公开，可匿名拉取。GitHub Actions 使用仓库自带的 `GITHUB_TOKEN` 发布，不需要额外创建 PAT。

#### 本地构建

1. 确保已安装 Docker 和 Docker Compose

2. 配置管理员账号
```bash
umask 077
mkdir -p data/certs
cp Server/config.yaml.example data/config.yaml
chmod 700 data data/certs
chmod 600 data/config.yaml
# 编辑 data/config.yaml 设置管理员用户名和密码
```

3. 一键启动
```bash
chmod +x docker-start.sh
./docker-start.sh
```

4. 访问 `http://localhost:28080`

### 方式二：本地部署

#### 环境要求

- Go 1.25+
- Node.js 20.19+
- npm（使用仓库中的 `package-lock.json`）

### 安装步骤

1. 克隆项目
```bash
git clone https://github.com/willamblack/cloudflare-tools.git
cd cloudflare-tools
```

2. 配置管理员账号
```bash
umask 077
cp Server/config.yaml.example Server/config.yaml
chmod 600 Server/config.yaml
# 编辑 Server/config.yaml，必须替换 CHANGE_ME
```

3. 安装 acme.sh（用于申请 SSL 证书）
```bash
chmod +x install-acme.sh
./install-acme.sh
source ~/.bashrc
```

4. 生产构建
```bash
chmod +x build.sh
./build.sh
```

构建完成后会在 `releases/` 目录生成可执行文件。

### 使用方法

1. 启动程序后访问 `http://localhost:8080`
2. 使用配置的管理员账号登录
3. 在账号管理中添加 CloudFlare API 密钥
4. 开始使用各项批量操作功能

## 配置说明

### config.yaml

```yaml
admin:
  username: 'admin'
  password: 'your-secure-password'
```

启动时按顺序查找 `${DATA_DIR}/config.yaml`、`config/config.yaml`、`config.yaml`。以下环境变量可覆盖文件配置：

- `ADMIN_USERNAME`：管理员用户名
- `ADMIN_PASSWORD`：管理员密码；默认示例密码会被拒绝
- `JWT_SECRET`：JWT HMAC 密钥，至少 32 字符
- `DATA_DIR`：`config.yaml`、`accounts.json` 和 `certs/` 的持久化目录

管理员密码目前以明文保存在 `config.yaml`；程序会拒绝组或其他用户可读的配置文件，请设为 `0600`，目录设为 `0700`。`Server/config.yaml`、`data/config.yaml` 和 `data/` 运行数据不再纳入 Git 跟踪；仓库仅保留 `Server/config.yaml.example`。

账号 Global API Key 目前以明文保存在 `${DATA_DIR}/accounts.json`，程序以 `0600` 权限原子更新，API 列表与编辑界面不会回传现有 Key。`0600` 不是静态加密：宿主机管理员、容器 root、拥有 Docker 权限的人，以及未加密备份仍可读取。请限制 Docker/宿主机访问，使用加密磁盘与加密备份，并通过 HTTPS 反向代理访问 Web UI；不要把 8080 端口直接暴露在公网。证书 ZIP 包含私钥，只能在登录后下载。

### CloudFlare API 密钥

需要在 CloudFlare 控制台获取:
1. 登录 CloudFlare
2. 进入 "我的个人资料" > "API 令牌"
3. 查看 "Global API Key"
4. 在工具中添加邮箱和 API Key

账号管理支持单个账号编辑：可以修改备注和邮箱；Global API Key 留空会保留旧 Key，填写新值才会替换。现有 Key 不会在浏览器中显示。

当前版本使用 Cloudflare Global API Key 兼容旧接口。该密钥权限很高，请使用专用 Cloudflare 账号、限制部署主机访问，并避免把 `data/accounts.json` 提交到 Git。

### 发布镜像

首次稳定发布：

```bash
git tag v0.01
git push origin v0.01
```

工作流会在 `linux/amd64` 与 `linux/arm64` 上构建镜像，并先在 GitHub 托管的 Linux runner 中真实启动单架构镜像，验证健康检查、首页、登录、鉴权与账号 API。稳定标签才会更新 `latest`。

### 健康检查

```bash
curl -fsS http://localhost:28080/healthz
```

成功时返回 `{"status":"ok"}`。

## 贡献

欢迎提交 Issue 和 Pull Request!

## 许可证

本项目采用 GNU Affero General Public License v3.0 (AGPL-3.0) 开源协议。

- ✅ 必须保留原作者署名
- ✅ 衍生作品必须开源
- ✅ 网络服务必须提供源代码
- ✅ 修改必须说明

详见 [LICENSE](LICENSE) 文件。

## 免责声明

本工具仅供学习和合法用途使用。使用本工具产生的任何后果由使用者自行承担。

## 联系方式

- Issues: [GitHub Issues](https://github.com/xkatld/Cloudflare-Tools/issues)
- 项目地址: https://github.com/xkatld/Cloudflare-Tools

---

⭐ 如果这个项目对你有帮助,请给个 Star!
