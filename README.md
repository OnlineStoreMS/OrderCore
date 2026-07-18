# OrderCore（订单中心）

OSMS 统一订单中心（OMS）：汇聚多渠道订单，完成履约分配与物流回传。

| 项 | 值 |
|---|---|
| Go module | `ordercore` |
| API | `:8098` |
| Web | `:5182` |
| 数据库 | PostgreSQL（库/用户 `ordercore`） |
| UserCore app | `ordercore`（`order:read` / `order:write`） |

## 能力（M0）

- **订单汇聚**
  - 电商订单：从 StoreSyncAgent（快递助手）同步
  - 门店销售：从 StoreCore 同步
  - 手工订单：后台直接创建
  - 微信小程序商城：来源预留 `wx_mall`
- **分配管理**（三种类型）
  - `self_ship` 自营发货
  - `dropship` 代发发货
    - `kdzs_factory`：快递助手厂家代发（推送即可，无需填单号）
    - `osms_supplier`：OSMS 供应商代发（线下沟通后手动填单号）
  - `purchase_then_ship` 采购发货（先采购到货再自营发出）
- **供应商 ↔ 厂家绑定**：标准化管理快递助手厂家
- **物流回传**：记录发货单号后回传来源模块（电商 → StoreSyncAgent `ship-callback`）

## 数据持久化

所有订单流转均写入 PostgreSQL：

| 表 | 内容 |
|---|---|
| `orders` / `order_items` / `order_addresses` | 订单主数据 |
| `order_status_logs` | 状态流转流水（建单/同步/分配/发货） |
| `order_shipments` | 发货单与物流回传结果 |
| `supplier_source_bindings` | 供应商↔厂家绑定 |

分配、发货等关键路径使用 **事务** 保证「状态变更 + 流水 + 发货单」原子落库。

初始化库（需 postgres 超级用户）：

```bash
make init-db APP_PASSWORD='你的密码'
# 或
./deploy/setup_db.sh '你的密码'
PGHOST=127.0.0.1 ./deploy/fix_db_permissions.sh
```

## 本地开发

```bash
# API（默认连接本地 PG）
go run ./cmd/api -config configs/config.local.yaml

# Web
cd web && npm install && npm run dev
```

SSO：UserCore 应用中心跳转 `/auth/callback?token=...`。

## 主要 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/admin/orders` | 订单列表 |
| POST | `/api/v1/admin/orders/manual` | 手工建单 |
| POST | `/api/v1/admin/orders/ingest` | 外部入库 |
| POST | `/api/v1/admin/orders/:id/allocate` | 分配 |
| POST | `/api/v1/admin/orders/:id/ship` | 发货/填单号 |
| POST | `/api/v1/admin/sync/kdzs` | 同步电商订单 |
| POST | `/api/v1/admin/sync/store` | 同步门店订单 |
| GET/POST | `/api/v1/admin/supplier-bindings` | 厂家绑定 |

## 集成依赖

- StoreSyncAgent `:8097` — 拉单、推厂家、物流回传
- StoreCore `:8094` — 门店销售单同步
- SupplyCore `:8092` — 供应商主数据（绑定引用其 ID）
