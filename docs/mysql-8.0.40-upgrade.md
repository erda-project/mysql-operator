# MySQL 8.0.40 多架构镜像升级

## 背景

MySQL 8.0.38 在表空间路径校验阶段可能因 `signal 11` 崩溃，升级到 8.0.40
可规避该问题。ERDA 的 MySQL Operator 使用 `mylet` 镜像运行数据库，因此需要
重新构建包含 MySQL 8.0.40 的 `mylet` 镜像。

## 镜像构建

`Dockerfile-mylet-8.0` 同时支持 `linux/amd64` 和 `linux/arm64`：

```sh
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -f Dockerfile-mylet-8.0 \
  -t registry.erda.cloud/erda-addons/mylet:8.0.40 \
  --push .
```

构建参数 `TARGETARCH` 由 Buildx 按目标平台自动设置，用于选择 ERDA 对应架构
的 Go 构建镜像。MySQL 使用同时提供 amd64/arm64 的官方 `mysql:8.0.40`，
xtrabackup 从同时提供双架构的 `percona/percona-xtrabackup:8.0.35` 复制。

已构建并推送：

```text
registry.erda.cloud/erda-addons/mylet:8.0.40
sha256:498ec6f4028f094d5be88e4b0345c95df5f0d446b17df5c83ce5704c3f631d70
```

该清单包含 `linux/amd64` 和 `linux/arm64` 两个平台。

## Operator 升级

保持 CRD 和业务 CR 的版本值不变，operator 将 `v8.0` 映射到上述镜像。发布
新的 operator 镜像
`registry.erda.cloud/erda-addons/myctl:v1.2-mysql-8.0.40`：

```text
sha256:3f498461cac73d567d61fe032807a7a4da08bc80218c32cd6f7dc5175976b1f0
```

该 operator 镜像同时包含 `linux/amd64` 和 `linux/arm64` 两个平台。

```sh
kubectl set image deployment/myctl \
  myctl=registry.erda.cloud/erda-addons/myctl:v1.2-mysql-8.0.40
kubectl rollout status deployment/myctl
```

现有 MySQL 资源只需保持：

```yaml
spec:
  version: v8.0
```

如果该资源已经显式固定了旧 `registry.erda.cloud/erda/mylet:` 默认镜像，
operator 会自动迁移到 `mylet:8.0.40`。其他自定义镜像保持不变。如需手动清理旧值：

```sh
kubectl -n project-1000324-test patch mysql <mysql资源名> --type json \
  -p '[{"op":"remove","path":"/spec/image"}]'
```

这是移除旧固定值，不会覆盖或改写 registry 中的镜像标签。

## 升级前检查

1. 对 MySQL PVC 创建快照或完整备份。
2. 记录当前 `Mysql` 资源的 `spec.image`，用于回滚。
3. 确认新镜像已推送，并同时包含 amd64 和 arm64 清单。
4. 升级后检查 Pod、`mysqld --version`、InnoDB 启动日志和业务连接。

回滚时将 `spec.image` 恢复为升级前的镜像，并确认 Pod 已重新创建。
