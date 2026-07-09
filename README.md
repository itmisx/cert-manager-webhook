# cert-manager-webhook

[English](./README.en.md) | 简体中文

为**阿里云、腾讯云、华为云、百度智能云** DNS 实现的 cert-manager ACME DNS-01 校验 Webhook——cert-manager 未内置这些国内厂商。一个可插拔的二进制即支持全部四家(按 `solverName` 切换),使用各厂商官方且在维护的 SDK,幂等且并发安全,并可签发通配符证书。

| 厂商 | `solverName` | SDK |
|------|--------------|-----|
| 阿里云 DNS(AliDNS) | `alidns` | `alidns-20150109/v5` |
| 腾讯云 DNS(DNSPod) | `tencentcloud` | `tencentcloud-sdk-go`(dnspod v20210323) |
| 华为云 DNS | `huaweicloud` | `huaweicloud-sdk-go-v3`(dns v2) |
| 百度智能云 DNS | `baiducloud` | `bce-sdk-go`(dns) |

> ⚠️ 首次使用请先读文末的[注意事项](#注意事项)——两条最常见的坑(用了 staging 导致证书不被信任、证书只认 SAN 不认 CN)会让你白忙一场。

## 前置条件

- Kubernetes 集群(v1.27+)
- 已安装 [cert-manager](https://cert-manager.io/docs/installation/) **v1.0+**(chart 使用 `cert-manager.io/v1` API,并依赖其 `cainjector` 注入 CA;建议用受支持的较新版本)
- 域名托管在受支持的某家云 DNS 上,并备好该厂商的 API 凭据:
  - **阿里云**:RAM 子用户 + `AliyunDNSFullAccess`
  - **腾讯云**:CAM 子账号 + `QcloudDNSPodFullAccess`
  - **华为云**:AK/SK,账号具备 **DNS Administrator** 角色
  - **百度智能云**:AK/SK,账号具备云解析 DNS 权限

## 安装

镜像和 Helm chart 都托管在 GitHub Container Registry(GHCR),由[发布工作流](./.github/workflows/release.yml)在打 tag 时自动发布,**无需 clone 仓库**即可安装(Helm 3.8+ 原生支持 OCI):

```bash
helm install cert-manager-webhook-dns \
  oci://ghcr.io/itmisx/charts/cert-manager-webhook-dns \
  --namespace cert-manager \
  --set groupName=acme.itmisx.com
```

默认安装最新版本;如需固定版本,追加 `--version <x.y.z>`。chart 默认使用镜像 `ghcr.io/itmisx/cert-manager-webhook`(标签取 chart 的 `appVersion`),无需额外设置。`groupName` 可为任意你能控制的域名,只需与每个 Issuer 的 `webhook.groupName` **保持一致**。

> 想从源码安装(开发用):`git clone` 后 `helm install cert-manager-webhook-dns ./deploy/cert-manager-webhook-dns -n cert-manager --set groupName=acme.itmisx.com`。

## 快速开始

以下以 **AliDNS** 为例;其他厂商步骤相同,按[配置项参考](#配置项参考)替换 `solverName` 与 `config` 即可。[`examples/`](./examples/) 目录提供了一套完整可改的示例。

### 1. 创建凭据 Secret

对 ClusterIssuer 而言,Secret 需放在 cert-manager 所在命名空间:

```bash
kubectl -n cert-manager create secret generic alidns-credentials \
  --from-literal=access-key-id=你的_ACCESS_KEY_ID \
  --from-literal=access-key-secret=你的_ACCESS_KEY_SECRET
```

### 2. 创建 ClusterIssuer

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt
spec:
  acme:
    # 建议先用 staging 测试:额度宽松,但证书不被浏览器信任。
    # 验证通过后,改用下面注释掉的生产地址,并删除旧证书 Secret 重新签发。
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    # server: https://acme-v02.api.letsencrypt.org/directory
    email: you@example.com
    privateKeySecretRef:
      name: letsencrypt-account-key
    solvers:
      - dns01:
          webhook:
            groupName: acme.itmisx.com
            solverName: alidns
            config:
              regionId: cn-hangzhou
              accessKeyIdSecretRef:
                name: alidns-credentials
                key: access-key-id
              accessKeySecretSecretRef:
                name: alidns-credentials
                key: access-key-secret
```

### 3. 申请证书

`dnsNames` 会成为证书的 SAN(浏览器只认它);DNS-01 还能签发通配符证书:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-com-tls
  namespace: default
spec:
  secretName: example-com-tls
  issuerRef:
    name: letsencrypt
    kind: ClusterIssuer
  dnsNames:
    - example.com
    - www.example.com
    - "*.example.com"
```

## 配置项参考

`groupName` 须与 Helm 的 `groupName` 一致;`solverName` 决定用哪个厂商。凭据一律来自校验命名空间里的 Kubernetes Secret(也可内联,但**仅限本地 `go test`**,切勿写进集群清单)。

### `solverName: alidns`(阿里云)

| 字段                          | 类型   | 必填  | 默认值                | 说明 |
|------------------------------|--------|-------|----------------------|------|
| `accessKeyIdSecretRef`       | object | 是    | —                    | 指向 AccessKey **ID** 的 Secret 引用(`key` 默认 `access-key-id`)。 |
| `accessKeySecretSecretRef`   | object | 是    | —                    | 指向 AccessKey **Secret** 的 Secret 引用(`key` 默认 `access-key-secret`)。 |
| `regionId`                   | string | 否    | *(不设)*              | 可选区域,例如 `cn-hangzhou`。DNS 是全局服务。 |
| `endpoint`                   | string | 否    | `alidns.aliyuncs.com`| 覆盖 API 接入点。 |
| `ttl`                        | int    | 否    | `600`                | TXT 记录 TTL(秒),强制不小于 600(免费版下限)。 |

### `solverName: tencentcloud`(腾讯云 / DNSPod)

| 字段                  | 类型   | 必填  | 默认值    | 说明 |
|----------------------|--------|-------|-----------|------|
| `secretIdSecretRef`  | object | 是    | —         | 指向 **SecretId** 的 Secret 引用(`key` 默认 `secret-id`)。 |
| `secretKeySecretRef` | object | 是    | —         | 指向 **SecretKey** 的 Secret 引用(`key` 默认 `secret-key`)。 |
| `region`             | string | 否    | *(不设)*  | 可选;DNSPod 是全局服务。 |
| `ttl`                | int    | 否    | `600`     | TXT 记录 TTL(秒)。 |

### `solverName: huaweicloud`(华为云)

| 字段                  | 类型   | 必填  | 默认值       | 说明 |
|----------------------|--------|-------|--------------|------|
| `accessKeySecretRef` | object | 是    | —            | 指向 **Access Key(AK)** 的 Secret 引用(`key` 默认 `access-key`)。 |
| `secretKeySecretRef` | object | 是    | —            | 指向 **Secret Key(SK)** 的 Secret 引用(`key` 默认 `secret-key`)。 |
| `region`             | string | 否    | `cn-north-4` | DNS 接入点区域。 |
| `ttl`                | int    | 否    | `300`        | TXT 记录 TTL(秒)。 |

### `solverName: baiducloud`(百度智能云)

| 字段                  | 类型   | 必填  | 默认值             | 说明 |
|----------------------|--------|-------|--------------------|------|
| `accessKeySecretRef` | object | 是    | —                  | 指向 **Access Key** 的 Secret 引用(`key` 默认 `access-key`)。 |
| `secretKeySecretRef` | object | 是    | —                  | 指向 **Secret Key** 的 Secret 引用(`key` 默认 `secret-key`)。 |
| `endpoint`           | string | 否    | `dns.baidubce.com` | 覆盖 API 接入点。 |
| `ttl`                | int    | 否    | `300`              | TXT 记录 TTL(秒)。 |

## 验证与排错

确认 webhook 已注册,并观察签发过程:

```bash
kubectl get apiservice v1alpha1.acme.itmisx.com   # 应为 Available=True
kubectl describe certificate example-com-tls
kubectl describe challenge                          # DNS-01 的 presenting 进度
kubectl -n cert-manager logs deploy/cert-manager-webhook-dns
```

查看已签发证书的签发者与 SAN(排查 staging 陷阱最快的方式):

```bash
kubectl get secret example-com-tls -o jsonpath='{.data.tls\.crt}' \
  | base64 -d | openssl x509 -noout -issuer -subject -ext subjectAltName
```

| 现象 | 可能原因 | 解决办法 |
|------|----------|----------|
| 浏览器提示"不受信任";签发者含 `(STAGING)` 或挂在 staging 根下的 `YR2` | 用了 Let's Encrypt **staging** | 把 `spec.acme.server` 改成生产地址,删掉旧证书 Secret,重新签发 |
| `NET::ERR_CERT_COMMON_NAME_INVALID` | 主机名不在 **SAN** 里 | 把它加入 `spec.dnsNames` |
| 日志出现鉴权错误(`SignatureDoesNotMatch`、`AuthFailure`、`APISIG.*` 等) | 凭据错误或带了多余空白 | 重建 Secret(webhook 本身已会去除首尾空白) |
| `... is not contained in zone ...` | 厂商上托管的域不是该名称的权威域 | 在对应厂商上托管正确的域 |
| challenge 卡在 "presenting" | 凭据缺少 DNS 写权限 | 授予该厂商的 DNS 完全访问策略(见[前置条件](#前置条件)) |

## 架构

一个 webhook 二进制为**每个厂商注册一个 solver**,同挂在一个 Kubernetes API 组下,由 Issuer 的 `solverName` 选择。通用的 `webhook.Solver` 适配层把 cert-manager 接口转接到一个极小的 `provider.Provider` 接口,因此**接入新厂商 = 新增一个包 + 在 `main.go` 加一行**,基础设施完全不用改。

```
cert-manager-webhook/
├── cmd/webhook/            # main():注册各厂商,启动 API server
├── internal/
│   ├── dnsutil/            # FQDN → (zone, RR) 拆分工具(与厂商无关)
│   ├── provider/           # Provider 接口 + 各厂商实现(alidns / tencentcloud / huaweicloud / baiducloud)
│   └── webhook/            # 通用 cert-manager Solver → Provider 适配层
├── deploy/cert-manager-webhook-dns/   # Helm chart
├── examples/               # 可直接改的 Issuer / Certificate / Secret 示例
└── testdata/               # cert-manager 一致性测试夹具
```

新增厂商的详细步骤见 [docs/adding-a-provider.md](./docs/adding-a-provider.md)。

## 开发

```bash
make test        # 快速单元测试,无需集群
make build       # 构建 ./bin/webhook
make vet         # go vet
make helm-lint   # lint chart(需要 helm)
```

**一致性测试**:cert-manager 官方 DNS-01 一致性套件会访问**真实 DNS**,用构建 tag 隔离、默认跳过。运行前填好 `testdata/<provider>-solver/config.json` 与含真实凭据的 `secret.yaml`(参考 `testdata/alidns-solver/README.md`),然后:

```bash
export TEST_ZONE_NAME="example.com."   # 结尾的点不能省
export TEST_PROVIDER=alidns            # 或 tencentcloud / huaweicloud / baiducloud
make test-conformance                  # 通过 setup-envtest 下载 envtest 二进制并运行
```

## 注意事项

<details>
<summary><b>点击展开:两条最容易导致「证书不受信任 / 浏览器不识别」的坑(强烈建议阅读)</b></summary>

<br>

### 坑 1:用了 staging 环境 → 证书不受信任(`YR2` / `CN` 的困惑)

几乎所有 cert-manager DNS webhook 教程都照抄了这个 ACME server 地址:

```
https://acme-staging-v02.api.letsencrypt.org/directory   ❌ staging —— 不受信任
```

Let's Encrypt 的 **staging(测试)环境**是用来做测试的。它签发的证书由**故意设置为不受信任的假根证书**签名,任何浏览器和操作系统都不会内置这些根,因此浏览器一定会报安全警告。查看这类证书会看到签发者类似 **`(STAGING) Ersatz Emmer YR2`**,并向上链接到 **`(STAGING) Yonder Yam Root YR`**。

> **"YR2" 是什么?** 2025 年 11 月 Let's Encrypt 推出了全新的 "Generation Y" CA 体系,`YR1`/`YR2`/`YR3` 是其中的 RSA 中间证书。在**生产环境**中它们合法可信;而在 **staging** 中,同名的 `(STAGING) … YR2` 是故意不受信任的。看到签发者是 `YR2` **本身不是问题**——真正的问题几乎总是:你用的是 **staging** 环境。

**解决办法**:把 `server` 换成生产环境地址并重新签发:

```
https://acme-v02.api.letsencrypt.org/directory           ✅ 生产 —— 受信任
```

只在调试阶段用 staging,调通后立即切回生产(调试用的 staging Issuer 见 [`examples/clusterissuer-letsencrypt-staging.yaml`](./examples/clusterissuer-letsencrypt-staging.yaml))。

> 生产环境的新根(Gen-Y / Root YR)默认已交叉签名到广受信任的 ISRG Root X1,通常无需处理;如想显式锁定兼容链,可在 Issuer 上设置 `preferredChain: "ISRG Root X1"`。详见 [Let's Encrypt 信任链](https://letsencrypt.org/certificates/)。

### 坑 2:Common Name vs. SAN → Chrome/Safari 不识别证书

从 **Chrome 58(2017)** 开始,浏览器**完全忽略证书的 Common Name(CN)**,只根据 **Subject Alternative Names(SAN)** 校验主机名。一个只有 CN、没有匹配 SAN 的证书会报 `NET::ERR_CERT_COMMON_NAME_INVALID`。

在 cert-manager 中,SAN 来自 **`spec.dnsNames`**。所以务必:

```yaml
spec:
  dnsNames:            # ← 这些会变成 SAN;浏览器校验的是这里
    - example.com
    - www.example.com
  # commonName: example.com   # 可选;若设置,它也必须出现在 dnsNames 里
```

Let's Encrypt 一定会签发 SAN,所以只要用了正确的生产 Issuer + 一份 `dnsNames` 列表,上面两个问题都会消失。

</details>

## 许可证

[MIT](./LICENSE)
