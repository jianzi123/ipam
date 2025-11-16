# iBGP vs eBGP 深度分析 - 容器网络场景

## 目录

- [1. BGP 协议基础](#1-bgp-协议基础)
- [2. iBGP vs eBGP 核心区别](#2-ibgp-vs-ebgp-核心区别)
- [3. 节点应该用 iBGP 还是 eBGP？](#3-节点应该用-ibgp-还是-ebgp)
- [4. 多节点共享网段场景](#4-多节点共享网段场景)
- [5. FRR/Zebra 配置方案](#5-frrzebra-配置方案)
- [6. 生产环境最佳实践](#6-生产环境最佳实践)

---

## 1. BGP 协议基础

### 1.1 什么是 BGP？

BGP (Border Gateway Protocol) 是**互联网的路由协议**，用于在不同自治系统（AS）之间交换路由信息。

```
AS (Autonomous System):
  - 独立的路由域
  - 拥有独立的 AS 号（ASN）
  - 内部使用统一的路由策略
```

### 1.2 BGP 的两种类型

| 类型 | 全称 | 使用场景 | AS 关系 |
|------|------|----------|---------|
| **eBGP** | External BGP | AS 之间 | 不同 AS |
| **iBGP** | Internal BGP | AS 内部 | 相同 AS |

---

## 2. iBGP vs eBGP 核心区别

### 2.1 本质区别

```
┌─────────────────────────────────────────────────────┐
│                    AS 65001                          │
│                                                      │
│   ┌──────┐    iBGP     ┌──────┐    iBGP    ┌──────┐│
│   │Router│ ←─────────→ │Router│ ←────────→ │Router││
│   │  A   │             │  B   │            │  C   ││
│   └───┬──┘             └───┬──┘            └───┬──┘│
└───────┼────────────────────┼─────────────────────┼──┘
        │ eBGP               │ eBGP                │ eBGP
        │                    │                     │
    ┌───▼──┐            ┌───▼──┐              ┌───▼──┐
    │AS    │            │AS    │              │AS    │
    │65002 │            │65003 │              │65004 │
    └──────┘            └──────┘              └──────┘
```

### 2.2 关键差异表

| 特性 | eBGP | iBGP |
|------|------|------|
| **AS 号** | 不同 AS | 相同 AS |
| **下一跳 (Next Hop)** | 自动更改为自己 | **保持原始下一跳** ⚠️ |
| **AS Path** | 添加自己的 AS | **不修改** |
| **TTL** | 默认 1 (直连) | 可以多跳 |
| **路由传播** | 直接传播 | **不传播给其他 iBGP peer** ⚠️ |
| **Loop 防止** | AS Path | **Split Horizon** |
| **典型拓扑** | 点对点 | Full Mesh 或 RR |

### 2.3 最重要的三个区别

#### **区别 1: 下一跳处理**

**eBGP**:
```
Router A (10.0.1.1) 发布路由 192.168.1.0/24
  ↓ eBGP
Router B 接收到:
  - Prefix: 192.168.1.0/24
  - Next Hop: 10.0.1.1 (Router A 的 IP) ✅ 自动修改
```

**iBGP**:
```
Router A (10.0.1.1) 从外部学到路由 192.168.1.0/24 (next-hop: 8.8.8.8)
  ↓ iBGP
Router B 接收到:
  - Prefix: 192.168.1.0/24
  - Next Hop: 8.8.8.8 ⚠️ 保持原始！
  - 问题: Router B 可能无法到达 8.8.8.8！
```

**解决方案**: 手动配置 `next-hop-self`
```
router bgp 65001
  neighbor 10.0.1.2 next-hop-self
```

#### **区别 2: 路由传播规则**

**eBGP**: 收到的路由会**自动传播**给所有 BGP peer（eBGP 和 iBGP）

**iBGP**: 从 iBGP peer 收到的路由**不会**传播给其他 iBGP peer（防环机制）

```
场景: 三个 iBGP 路由器

Router A ──iBGP──→ Router B ──iBGP──→ Router C
            ↑                            ↓
            └────────── iBGP ────────────┘

问题:
1. A 发布路由给 B
2. B 不会传给 C（iBGP split horizon）
3. C 收不到路由！❌

解决方案:
- Full Mesh: 每个路由器之间都建立 iBGP（n*(n-1)/2 条连接）
- Route Reflector: 指定 RR 负责反射路由
- Confederation: 分层 AS
```

#### **区别 3: AS Path**

**eBGP**: 每经过一个 AS，就在 AS_PATH 中添加自己的 AS 号
```
AS 65001 → AS 65002 → AS 65003

AS_PATH 变化:
  - 从 65001 发出: []
  - 到达 65002: [65001]
  - 到达 65003: [65002, 65001]

用途: 防环（如果看到自己的 AS 号，丢弃路由）
```

**iBGP**: AS_PATH **不变**
```
同一个 AS 内部，AS_PATH 保持不变
```

---

## 3. 节点应该用 iBGP 还是 eBGP？

### 3.1 场景分析

在容器网络中，我们有：

```
Kubernetes 集群:
  - 节点: Node-1, Node-2, Node-3, ...
  - Leaf 交换机: Leaf-1, Leaf-2, ...
  - 需要: Node ↔ Leaf 建立 BGP peering
```

**问题**: Node 到 Leaf 应该用 iBGP 还是 eBGP？

### 3.2 两种方案对比

#### **方案 A: 全部使用 iBGP**

```
配置:
  - 所有 Node 使用相同 AS: AS 65001
  - 所有 Leaf 使用相同 AS: AS 65001
  - Node ↔ Leaf: iBGP peering
```

**问题**:

1. **需要 Full Mesh 或 Route Reflector**
```
3 个 Leaf + 100 个 Node = 103 个路由器
Full Mesh: 103 * 102 / 2 = 5253 条 BGP 连接！❌
```

2. **下一跳问题**
```
Node-1 发布路由 10.244.1.5/32 (next-hop: Node-1 IP)
  ↓ iBGP (保持下一跳)
Leaf-1 接收: 10.244.1.5/32 (next-hop: Node-1 IP)
  ↓ iBGP (保持下一跳)
Leaf-2 接收: 10.244.1.5/32 (next-hop: Node-1 IP) ⚠️
  问题: Leaf-2 可能无法直接到达 Node-1！
```

需要配置 `next-hop-self` 在每一跳

3. **路由传播限制**
```
Node-1 ──iBGP──→ Leaf-1 ──iBGP──→ Leaf-2
                           ↓
                      ❌ 路由不传播！
```

需要 Route Reflector:
```
配置 Spine 为 RR:

Node-1 ──iBGP──→ Leaf-1 ──iBGP──→ Spine-1 (RR)
                                     ↓ 反射
                                  Leaf-2
```

**优点**: AS 号管理简单

**缺点**:
- 需要 RR（增加复杂度）
- Full Mesh 不可扩展
- 下一跳处理复杂
- 配置繁琐

#### **方案 B: 使用 eBGP （推荐）✅**

```
配置:
  - 每个 Node 独立 AS: AS 4200000001, 4200000002, ...
  - 每个 Leaf 独立 AS: AS 65001, 65002, ...
  - Node ↔ Leaf: eBGP peering
```

**优点**:

1. **无需 Full Mesh**
```
Node-1 (AS 4200000001) ──eBGP──→ Leaf-1 (AS 65001)
Node-2 (AS 4200000002) ──eBGP──→ Leaf-1 (AS 65001)
Node-3 (AS 4200000003) ──eBGP──→ Leaf-1 (AS 65001)

每个 Node 只需连接到 1-2 个 Leaf（冗余）
无需 Node 之间互连！✅
```

2. **下一跳自动更新**
```
Node-1 发布路由 10.244.1.5/32
  ↓ eBGP (自动设置 next-hop = Leaf-1 自己)
Leaf-1 接收: 10.244.1.5/32 (next-hop: Leaf-1 自己)
  ↓ eBGP to Spine
Spine 接收: 10.244.1.5/32 (next-hop: Leaf-1)
  ✅ 正确！
```

3. **路由自动传播**
```
Node-1 ──eBGP──→ Leaf-1 ──eBGP──→ Spine ──eBGP──→ Leaf-2
                                                     ↓
                                                 ✅ 正常传播
```

4. **快速收敛**
```
eBGP 默认 Hold Time: 180s (可调整为 90s)
+ BFD: 亚秒级故障检测
= 快速收敛 < 1s
```

**缺点**: 需要管理大量 AS 号（可接受，使用 4 字节私有 AS）

### 3.3 结论

**容器网络场景推荐：全 eBGP 架构** ✅

```
理由:
1. 扩展性好 - 支持数万节点
2. 配置简单 - 无需 RR
3. 收敛快 - eBGP 特性
4. 下一跳自动处理
5. 符合业界实践 (Calico, Cilium, kube-router)
```

---

## 4. 多节点共享网段场景

### 4.1 场景描述

**问题**: 使用拓扑感知 IPAM，同一个 TOR 下的多个节点共享一个网段池

```
TOR-1 (网段池: 10.244.1.0/24)
  ├─ Node-1: 分配 10.244.1.1 - 10.244.1.50
  ├─ Node-2: 分配 10.244.1.51 - 10.244.1.100
  └─ Node-3: 分配 10.244.1.101 - 10.244.1.150

每个节点都会发布 /32 路由:
  - Node-1: 10.244.1.1/32, 10.244.1.2/32, ...
  - Node-2: 10.244.1.51/32, 10.244.1.52/32, ...
  - Node-3: 10.244.1.101/32, 10.244.1.102/32, ...
```

### 4.2 是否需要广播节点上的网段？

**答案**: **不需要广播整个网段，只需广播 /32 路由** ✅

**原因**:

1. **精确路由优先**
```
路由选择规则: 最长前缀匹配 (Longest Prefix Match)

路由表:
  - 10.244.1.0/24   via Leaf-1    (汇总路由)
  - 10.244.1.5/32   via Node-1    (精确路由)
  - 10.244.1.52/32  via Node-2    (精确路由)

访问 10.244.1.5:
  ✅ 匹配 /32 路由 → 转发到 Node-1
  ❌ 不匹配 /24 路由（前缀更短）
```

2. **避免路由冲突**
```
如果广播网段:

Node-1 广播: 10.244.1.0/24 via Node-1
Node-2 广播: 10.244.1.0/24 via Node-2
Node-3 广播: 10.244.1.0/24 via Node-3

问题: 3 个等价路由！
结果: ECMP 随机选择 → 可能转发到错误的节点 ❌
```

3. **正确的方案**
```
节点层:
  - 每个节点只发布自己的 /32 路由
  - Node-1: 10.244.1.5/32 via Node-1 ✅
  - Node-2: 10.244.1.52/32 via Node-2 ✅

Leaf 层 (可选):
  - Leaf 可以向 Spine 发布汇总路由
  - Leaf-1: 10.244.1.0/24 via Leaf-1
  - 用途: 减少 Spine 路由表大小
  - 注意: Leaf 仍然维护所有 /32 路由（用于精确转发）
```

### 4.3 BGP 协议如何正确处理？

#### **场景 1: 不同节点发布不同的 /32 路由**

```
Node-1: 10.244.1.5/32  (AS Path: [4200000001])
Node-2: 10.244.1.52/32 (AS Path: [4200000002])

Leaf-1 接收:
  - 10.244.1.5/32  via Node-1 ✅
  - 10.244.1.52/32 via Node-2 ✅

✅ 正常工作，无冲突
```

#### **场景 2: 不同节点发布相同的 /32 路由（Anycast）**

```
特殊情况: 两个节点都运行相同服务，发布相同 IP

Node-1: 10.244.1.100/32 (服务 A)
Node-2: 10.244.1.100/32 (服务 A)

Leaf-1 接收:
  - 10.244.1.100/32 via Node-1 (AS Path: [4200000001])
  - 10.244.1.100/32 via Node-2 (AS Path: [4200000002])

BGP 行为:
  - 两条路由都保留（multipath）
  - 启用 ECMP 负载均衡
  - 流量分散到两个节点 ✅

用途:
  - Anycast DNS
  - Load Balancer VIP
  - Service Mesh
```

配置 (FRR):
```
router bgp 65001
  maximum-paths 32  # 允许最多 32 条等价路径
```

#### **场景 3: Leaf 汇总路由（推荐用于大规模）**

```
Node 层: 发布 /32 路由
  - Node-1 → Leaf-1: 10.244.1.5/32, 10.244.1.6/32, ...
  - Node-2 → Leaf-1: 10.244.1.52/32, 10.244.1.53/32, ...

Leaf 层: 汇总后发布到 Spine
  - Leaf-1 → Spine: 10.244.1.0/24 (汇总)
  - Leaf-1 内部: 仍保留所有 /32 路由 ✅

Spine 层: 仅看到汇总路由
  - 10.244.1.0/24 via Leaf-1
  - 10.244.2.0/24 via Leaf-2

优势:
  - Spine 路由表小（几百条 vs 几十万条）
  - 不影响 Leaf 下的精确转发
```

配置 (FRR Leaf):
```
router bgp 65001
  # 接收 /32 路由（从 Node）
  neighbor NODE_GROUP route-map IMPORT_NODE_ROUTES in

  # 向 Spine 发布汇总路由
  neighbor SPINE_GROUP aggregate-address 10.244.1.0/24 summary-only
```

### 4.4 最佳实践总结

| 层级 | 发布内容 | 原因 |
|------|---------|------|
| **Node → Leaf** | 仅 /32 路由 | 精确路由，避免冲突 |
| **Leaf → Spine** | /32 或汇总路由 | 小规模: /32<br>大规模: 汇总 |
| **Spine → Leaf** | 反射所有路由 | 全局路由视图 |

---

## 5. FRR/Zebra 配置方案

### 5.1 为什么选择 FRR？

| 方案 | 优势 | 劣势 |
|------|------|------|
| **BIRD** | 性能最高，配置灵活 | 配置语法复杂 |
| **FRR** | 全功能路由套件，类 Cisco | 占用资源略多 |
| **GoBGP** | Go 编写，易集成 API | 性能略低 |

**FRR (Free Range Routing)** = Quagga 的继承者，生产级路由软件

### 5.2 Node 节点 FRR 配置

#### **/etc/frr/frr.conf** (Node-1)

```bash
# ============================================
# FRR 配置 - Node-1
# ============================================

# 基础配置
hostname node-1
log syslog informational
service integrated-vtysh-config

# ============================================
# Zebra (内核路由管理)
# ============================================
# Zebra 负责:
# 1. 监听内核路由表
# 2. 将 BGP 学到的路由写入内核
# 3. 将内核路由导入 BGP

# 启用 IPv4 转发
ip forwarding

# 从内核导入直连路由（容器路由）
ip protocol connected route-map CONTAINER_ROUTES

# 路由映射: 只导入容器网段
route-map CONTAINER_ROUTES permit 10
  match interface cali+    # Calico veth pair
  match ip address prefix-list CONTAINER_IPS

ip prefix-list CONTAINER_IPS seq 10 permit 10.244.0.0/16 le 32

# ============================================
# BGP 配置
# ============================================
router bgp 4200000001
  # Router ID
  bgp router-id 192.168.1.10

  # 禁用默认的 IPv4 unicast (显式激活)
  no bgp default ipv4-unicast

  # BGP 定时器
  bgp bestpath as-path multipath-relax  # 允许不同 AS Path 的 ECMP
  timers bgp 30 90                       # keepalive 30s, hold 90s

  # 邻居组: Leaf 交换机
  neighbor LEAF peer-group
  neighbor LEAF remote-as external       # eBGP (每个 Leaf 不同 AS)
  neighbor LEAF ebgp-multihop 1         # 直连，TTL=1
  neighbor LEAF capability dynamic
  neighbor LEAF timers connect 10

  # 具体邻居
  neighbor 192.168.1.1 peer-group LEAF
  neighbor 192.168.1.1 remote-as 65001  # Leaf-1
  neighbor 192.168.1.1 description "Leaf-1"

  neighbor 192.168.1.2 peer-group LEAF
  neighbor 192.168.1.2 remote-as 65001  # Leaf-2 (backup)
  neighbor 192.168.1.2 description "Leaf-2"

  # IPv4 地址族
  address-family ipv4 unicast
    # 激活邻居
    neighbor LEAF activate

    # 导入: 接收所有容器路由
    neighbor LEAF route-map IMPORT_FROM_LEAF in

    # 导出: 只发布本节点的 /32 路由
    neighbor LEAF route-map EXPORT_TO_LEAF out

    # 从 Zebra 重分发路由 (容器路由)
    redistribute connected route-map CONTAINER_ROUTES

    # 启用 ECMP
    maximum-paths 32
  exit-address-family

# ============================================
# 路由策略
# ============================================

# 导入策略: 接收所有容器路由
route-map IMPORT_FROM_LEAF permit 10
  match ip address prefix-list CONTAINER_IPS

# 导出策略: 只发布 /32 路由
route-map EXPORT_TO_LEAF permit 10
  match ip address prefix-list CONTAINER_IPS
  match ip address prefix-len 32   # 仅 /32
  # 可选: 设置 BGP Community
  set community 65001:100

route-map EXPORT_TO_LEAF deny 20

# ============================================
# BFD (快速故障检测)
# ============================================
bfd
  profile leaf-profile
    detect-multiplier 3
    receive-interval 300
    transmit-interval 300
  !
!

router bgp 4200000001
  neighbor LEAF bfd profile leaf-profile

# ============================================
# 监控和调试命令
# ============================================
# 查看 BGP 状态:
#   vtysh -c "show ip bgp summary"
#
# 查看路由:
#   vtysh -c "show ip bgp"
#   vtysh -c "show ip route"
#
# 查看 BFD 会话:
#   vtysh -c "show bfd peers"
#
# 清空 BGP 会话:
#   vtysh -c "clear ip bgp *"
```

### 5.3 Leaf 交换机 FRR 配置

#### **/etc/frr/frr.conf** (Leaf-1)

```bash
# ============================================
# FRR 配置 - Leaf-1
# ============================================

hostname leaf-1
log syslog informational
service integrated-vtysh-config

ip forwarding

# ============================================
# BGP 配置
# ============================================
router bgp 65001
  bgp router-id 192.168.1.1

  no bgp default ipv4-unicast

  # 最大路径 (ECMP)
  bgp bestpath as-path multipath-relax
  maximum-paths 32

  # 邻居组: Spine 核心交换机
  neighbor SPINE peer-group
  neighbor SPINE remote-as 65000   # Spine AS
  neighbor SPINE ebgp-multihop 1

  neighbor 10.0.1.1 peer-group SPINE
  neighbor 10.0.1.1 description "Spine-1"

  neighbor 10.0.1.2 peer-group SPINE
  neighbor 10.0.1.2 description "Spine-2"

  # 邻居组: Node 计算节点 (动态邻居)
  bgp listen range 192.168.1.0/24 peer-group NODES
  neighbor NODES remote-as external   # eBGP
  neighbor NODES ebgp-multihop 1
  neighbor NODES maximum-prefix 10000  # 限制路由数量

  # IPv4 地址族
  address-family ipv4 unicast
    neighbor SPINE activate
    neighbor NODES activate

    # 接收 Node 路由
    neighbor NODES route-map IMPORT_FROM_NODE in

    # 向 Spine 发布路由
    neighbor SPINE route-map EXPORT_TO_SPINE out
    neighbor SPINE next-hop-self   # 修改下一跳为自己

    # 向 Node 发布路由
    neighbor NODES route-map EXPORT_TO_NODE out

    # ECMP
    maximum-paths 32
  exit-address-family

# ============================================
# 路由策略
# ============================================

# 从 Node 导入: 只接收 /32 容器路由
route-map IMPORT_FROM_NODE permit 10
  match ip address prefix-list CONTAINER_IPS
  match ip address prefix-len 32

ip prefix-list CONTAINER_IPS permit 10.244.0.0/16 le 32

# 向 Spine 导出: 选项 1 - 发布所有 /32 路由
route-map EXPORT_TO_SPINE permit 10
  match ip address prefix-list CONTAINER_IPS

# 向 Spine 导出: 选项 2 - 汇总路由 (大规模推荐)
# 取消注释以启用:
# route-map EXPORT_TO_SPINE permit 10
#   match ip address prefix-list AGGREGATES
#
# ip prefix-list AGGREGATES permit 10.244.1.0/24
#
# router bgp 65001
#   address-family ipv4 unicast
#     aggregate-address 10.244.1.0/24 summary-only

# 向 Node 导出: 发布所有路由
route-map EXPORT_TO_NODE permit 10
  match ip address prefix-list CONTAINER_IPS

# ============================================
# BFD
# ============================================
bfd
  profile default
    detect-multiplier 3
    receive-interval 300
    transmit-interval 300
  !
!

router bgp 65001
  neighbor SPINE bfd
  neighbor NODES bfd
```

### 5.4 验证和监控

#### **验证 BGP 会话**

```bash
# 查看 BGP 邻居状态
vtysh -c "show ip bgp summary"

# 输出示例:
# BGP router identifier 192.168.1.10, local AS number 4200000001
# Neighbor        V    AS   MsgRcvd MsgSent   TblVer  InQ OutQ  Up/Down  State/PfxRcd
# 192.168.1.1     4 65001     12345   12346        0    0    0 01:23:45       5432
# 192.168.1.2     4 65001     12340   12341        0    0    0 01:23:40       5430
#
# State: Established ✅
# PfxRcd: 接收的路由数量
```

#### **查看路由**

```bash
# 查看 BGP 路由表
vtysh -c "show ip bgp"

# 输出示例:
#    Network          Next Hop            Metric LocPrf Weight Path
# *> 10.244.1.5/32    0.0.0.0                  0         32768 i
# *> 10.244.2.10/32   192.168.1.1              0             0 65001 4200000002 i
# *  10.244.2.10/32   192.168.1.2              0             0 65001 4200000002 i
#
# *>: 最佳路由
# *:  备用路由
# i:  IGP (本地路由)

# 查看内核路由表
ip route show proto bgp

# 输出示例:
# 10.244.2.10 via 192.168.1.1 dev eth0 proto bgp metric 20
# 10.244.3.15 via 192.168.1.1 dev eth0 proto bgp metric 20
```

#### **查看 BFD 会话**

```bash
vtysh -c "show bfd peers"

# 输出示例:
# BFD Peers:
#   peer 192.168.1.1 vrf default
#     ID: 1234
#     Remote ID: 5678
#     Status: up ✅
#     Uptime: 1 day, 2:34:56
#     Diagnostics: ok
#     Remote diagnostics: ok
#     Peer Type: configured
#     RTT min/avg/max: 0.1/0.3/1.2 ms
```

#### **调试 BGP**

```bash
# 启用 BGP 调试
vtysh -c "debug bgp updates"
vtysh -c "debug bgp neighbor-events"

# 查看日志
tail -f /var/log/frr/frr.log

# 清空 BGP 会话 (重新建立)
vtysh -c "clear ip bgp *"
```

---

## 6. 生产环境最佳实践

### 6.1 AS 号规划

```
层级              | AS 号范围              | 数量
------------------|------------------------|--------
Spine (核心)      | AS 65000               | 1 (共享)
Leaf (接入)       | AS 65001 - 65099       | ~100
Node (计算)       | AS 4200000001 - 4200999999 | ~100万

注意:
- 使用 4 字节私有 AS (4200000000-4294967294)
- 避免与公网 AS 冲突
- 预留空间用于扩展
```

### 6.2 路由过滤

**防止路由泄露**:

```bash
# Node 配置: 只发布容器 /32
route-map EXPORT permit 10
  match ip address prefix-list CONTAINER_IPS
  match ip address prefix-len 32
route-map EXPORT deny 20

# Leaf 配置: 限制接收的路由数量
neighbor NODES maximum-prefix 10000 restart 5
```

### 6.3 ECMP 配置

```bash
# 启用 ECMP (多路径负载均衡)
router bgp <AS>
  bgp bestpath as-path multipath-relax
  maximum-paths 32  # 最多 32 条等价路径

# 内核配置
sysctl -w net.ipv4.fib_multipath_hash_policy=1  # L3+L4 哈希
```

### 6.4 故障检测和收敛

```bash
# BFD 配置 (< 1s 检测)
bfd
  profile production
    detect-multiplier 3      # 丢失 3 个包认为故障
    receive-interval 300     # 300ms 接收间隔
    transmit-interval 300    # 300ms 发送间隔
  !

# BGP 定时器
router bgp <AS>
  timers bgp 10 30  # keepalive 10s, hold 30s (更激进)
```

### 6.5 路由汇总策略

**小规模 (< 1000 节点)**:
```
✅ Node → Leaf: /32 路由
✅ Leaf → Spine: /32 路由
✅ Spine 路由表: ~10000 条

优势: 精确路由，最优路径
```

**大规模 (> 10000 节点)**:
```
✅ Node → Leaf: /32 路由
✅ Leaf → Spine: 汇总路由 (/24)
✅ Spine 路由表: ~100 条

优势: 减少 Spine 负担
注意: Leaf 仍保留所有 /32 路由
```

### 6.6 监控指标

```
关键指标:
1. BGP 会话状态 (Up/Down)
2. 路由数量 (Prefix Count)
3. 路由收敛时间
4. BFD 会话状态
5. ECMP 路径数量
6. 路由 Flapping 频率

告警阈值:
- BGP 会话断开 > 30s
- 路由数量突变 > 10%
- 收敛时间 > 5s
```

---

## 7. 总结

### 7.1 关键决策

| 问题 | 答案 | 原因 |
|------|------|------|
| **节点用 iBGP 还是 eBGP？** | **eBGP** ✅ | 扩展性、简单性、快速收敛 |
| **是否广播节点网段？** | **否，仅 /32** ✅ | 避免冲突，精确路由 |
| **Leaf 是否汇总？** | **可选** | 小规模: 否<br>大规模: 是 |
| **BGP 实现？** | **FRR** ✅ | 生产级，功能全，类 Cisco |

### 7.2 架构总结

```
完整架构:

Spine (AS 65000)
  ├─ iBGP Mesh (Spine 之间)
  ├─ eBGP to Leaf-1 (AS 65001)
  ├─ eBGP to Leaf-2 (AS 65002)
  └─ eBGP to Leaf-N (AS 6500N)

Leaf (AS 65001)
  ├─ eBGP to Spine
  ├─ eBGP to Node-1 (AS 4200000001)
  ├─ eBGP to Node-2 (AS 4200000002)
  └─ eBGP to Node-N (AS 420000000N)

Node (AS 4200000001)
  ├─ eBGP to Leaf-1
  ├─ eBGP to Leaf-2 (backup)
  └─ 发布: 10.244.1.5/32, 10.244.1.6/32, ... (仅 /32)

特点:
✅ 全 eBGP (除 Spine 内部 iBGP)
✅ 每节点独立 AS
✅ /32 路由发布
✅ ECMP 负载均衡
✅ BFD 快速检测
```

### 7.3 与 IPAM 集成

```
IPAM 拓扑      ←→  BGP 网络
-----------        -----------
Zone           ←→  (逻辑分组)
Pod            ←→  (逻辑分组)
TOR            ←→  Leaf 聚合点
Node           ←→  BGP Speaker

网段分配:
- IPAM: 为 Node 分配 IP (从 TOR 网段池)
- BGP: Node 发布分配到的 IP (/32 路由)
- 结果: 拓扑感知 + 动态路由 = 最优网络架构
```

---

## 参考资料

- [RFC 4271](https://www.rfc-editor.org/rfc/rfc4271.html) - BGP-4
- [RFC 4456](https://www.rfc-editor.org/rfc/rfc4456.html) - BGP Route Reflection
- [RFC 7938](https://www.rfc-editor.org/rfc/rfc7938.html) - Use of BGP for Routing in Large-Scale Data Centers
- [FRR Documentation](https://docs.frrouting.org/)
- [Calico BGP](https://docs.tigera.io/calico/latest/reference/architecture/overview)
