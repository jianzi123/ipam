# BGP 三层网络设计文档

> **重要补充**: 如果你对 iBGP 和 eBGP 的区别、以及多节点共享网段的路由策略有疑问，请先阅读：
> - **[BGP_IBGP_EBGP_ANALYSIS.md](BGP_IBGP_EBGP_ANALYSIS.md)** - iBGP vs eBGP 深度分析，包含详细的 FRR 配置

## 目录

- [1. 架构概述](#1-架构概述)
- [2. 网络拓扑](#2-网络拓扑)
- [3. BGP 设计](#3-bgp-设计)
- [4. 路由发布机制](#4-路由发布机制)
- [5. 数据包转发路径](#5-数据包转发路径)
- [6. 节点 BGP 配置](#6-节点-bgp-配置)
- [7. 与 IPAM 集成](#7-与-ipam-集成)
- [8. 实现方案](#8-实现方案)

---

## 1. 架构概述

### 1.1 设计目标

基于 BGP 构建三层网络，实现容器网络的 underlay 路由，取代传统的 overlay 方案（如 VXLAN）。核心特点：

- **纯三层路由**：无需 overlay 封装，降低网络复杂度
- **容器路由发布**：每个容器 IP 以 /32 路由发布到网络
- **ECMP 负载均衡**：利用 BGP 多路径实现流量均衡
- **快速收敛**：BGP 故障检测和路由收敛 < 1s

### 1.2 整体架构

```
                    ┌─────────────────────────────────────┐
                    │         Spine Layer (核心层)         │
                    │  ┌────────┐      ┌────────┐         │
                    │  │Spine-1 │      │Spine-2 │         │
                    │  │ AS 65000│     │ AS 65000│        │
                    │  └───┬────┘      └───┬────┘         │
                    └──────┼───────────────┼──────────────┘
                           │               │
              ┌────────────┼───────────────┼────────────┐
              │            │               │            │
    ┌─────────▼─────┐  ┌──▼──────┐  ┌────▼─────┐  ┌───▼──────┐
    │   Leaf-1      │  │ Leaf-2  │  │ Leaf-3   │  │ Leaf-N   │
    │   AS 65001    │  │AS 65002 │  │AS 65003  │  │AS 6500N  │
    └───────┬───────┘  └────┬────┘  └────┬─────┘  └────┬─────┘
            │               │            │             │
    ┌───────┴────────┐  ┌───┴──────┐  ┌─┴──────┐  ┌───┴──────┐
    │  TOR-1         │  │  TOR-2   │  │ TOR-3  │  │  TOR-N   │
    │  L2 Switch     │  │L2 Switch │  │L2Switch│  │L2 Switch │
    └───┬────┬───┬───┘  └──┬───┬───┘  └┬───┬───┘  └──┬───┬───┘
        │    │   │         │   │       │   │         │   │
    ┌───▼┐ ┌▼──┐│      ┌──▼┐ ┌▼──┐  ┌▼──┐│       ┌──▼┐ ┌▼──┐
    │Node1│Node2│      │Node│Node │  │Node│       │Node│Node│
    │     │     │      │ 3  │ 4  │  │ 5  │       │ N  │N+1 │
    └─────┘└─────┘     └────┘└────┘  └────┘       └────┘└────┘
       ↓      ↓          ↓     ↓      ↓            ↓     ↓
    [Pod IP] [Pod IP] [Pod IP]...                [Pod IP]...
    /32 routes published via BGP
```

---

## 2. 网络拓扑

### 2.1 三层架构

#### **Spine Layer (核心层)**
- **角色**: BGP Route Reflector (RR)
- **功能**:
  - 汇聚所有 Leaf 路由
  - 反射路由到所有 Leaf
  - 提供全局路由视图
- **AS 号**: 统一使用 AS 65000 (iBGP)
- **协议**:
  - Spine ↔ Leaf: eBGP
  - Spine ↔ Spine: iBGP (可选，用于冗余)

#### **Leaf Layer (接入层)**
- **角色**: 接入交换机 + BGP Speaker
- **功能**:
  - 连接 TOR 交换机
  - 从 Node 接收容器路由
  - 向 Spine 发布汇总路由
- **AS 号**: 每个 Leaf 独立 AS（AS 65001-6500N）
- **协议**:
  - Leaf ↔ Spine: eBGP
  - Leaf ↔ Node: eBGP

#### **TOR Layer (机架交换机)**
- **角色**: 纯二层交换机
- **功能**:
  - 连接物理节点
  - VLAN 隔离（可选）
  - 透传三层流量
- **协议**: 二层交换，无路由功能

#### **Node Layer (计算节点)**
- **角色**: 容器宿主机 + BGP Speaker
- **功能**:
  - 运行容器/Pod
  - 向 Leaf 发布容器 /32 路由
  - 接收外部流量并转发到容器
- **AS 号**: 每个节点独立 AS（AS 4200000000-4294967294，4字节私有 AS）
- **协议**:
  - Node ↔ Leaf: eBGP

### 2.2 为什么选择 eBGP？

| 方案 | 优势 | 劣势 | 适用场景 |
|------|------|------|----------|
| **全 iBGP** | 配置简单，同一 AS | 需要 full mesh 或 RR，扩展性差 | 小规模集群 |
| **全 eBGP** | 扩展性好，无需 RR，快速收敛 | AS 号管理复杂 | **大规模数据中心** ✅ |
| **混合 (iBGP + eBGP)** | 分层清晰 | 配置复杂 | 多数据中心 |

**本方案选择：Spine-Leaf-Node 全 eBGP**

原因：
1. **扩展性**: 支持上千节点，无需 full mesh
2. **快速收敛**: eBGP 收敛速度快（< 1s）
3. **简化管理**: 每层独立 AS，故障域隔离
4. **符合业界实践**: Calico、Cilium 等 CNI 均采用此方案

---

## 3. BGP 设计

### 3.1 AS 号规划

```
AS Tier          | AS Number Range        | Description
-----------------|------------------------|---------------------------
Spine (Core)     | AS 65000               | 固定，所有 Spine 共享
Leaf (Aggregation)| AS 65001 - 65099      | 每个 Leaf 独立 AS
Node (Compute)   | AS 4200000000 - 4200099999 | 4字节私有 AS，每节点独立
```

**AS 号分配示例**:
```
Spine-1: AS 65000
Spine-2: AS 65000

Leaf-1:  AS 65001
Leaf-2:  AS 65002
Leaf-3:  AS 65003

Node-1:  AS 4200000001
Node-2:  AS 4200000002
Node-N:  AS 420000000N
```

### 3.2 BGP Peering 关系

```
┌─────────────────────────────────────────────────────┐
│  Peering Type      │ Protocol │ Next Hop        │
│────────────────────│──────────│─────────────────│
│  Spine ↔ Spine     │ iBGP     │ Preserve (RR)   │
│  Spine ↔ Leaf      │ eBGP     │ Self            │
│  Leaf ↔ Node       │ eBGP     │ Self            │
└─────────────────────────────────────────────────────┘
```

**Peering 配置**:

1. **Spine ↔ Leaf**
   - Protocol: eBGP
   - Multihop: No (直连)
   - BFD: 启用（快速故障检测）
   - ECMP: 启用（多路径负载均衡）

2. **Leaf ↔ Node**
   - Protocol: eBGP
   - Multihop: No (直连)
   - BFD: 启用
   - Route Limit: 10000/节点（防止路由表爆炸）

### 3.3 路由策略

#### **路由过滤**

**Node → Leaf (出方向)**:
```
# 只允许发布容器 /32 路由
route-map NODE_TO_LEAF permit 10
  match ip address prefix-list CONTAINER_IPS

ip prefix-list CONTAINER_IPS permit 10.244.0.0/16 le 32
```

**Leaf → Spine (出方向)**:
```
# 可以发布容器路由 或 汇总路由
route-map LEAF_TO_SPINE permit 10
  match ip address prefix-list LEAF_AGGREGATES

# 选项 1: 发布所有 /32 路由（小规模）
ip prefix-list LEAF_AGGREGATES permit 10.244.0.0/16 le 32

# 选项 2: 仅发布 Leaf 汇总路由（大规模推荐）
ip prefix-list LEAF_AGGREGATES permit 10.244.1.0/24
```

**Spine → Leaf (出方向)**:
```
# 反射所有容器路由
route-map SPINE_TO_LEAF permit 10
  match ip address prefix-list ALL_CONTAINERS
```

#### **BGP 属性**

- **Local Preference**: 不使用（eBGP 忽略）
- **MED**: 不使用（简化设计）
- **AS Path Prepending**: 不使用（ECMP 优先）
- **Community**: 可选，用于策略标记

---

## 4. 路由发布机制

### 4.1 容器 IP 路由发布

**核心机制**: 每个容器 IP 以 **/32 主机路由** 发布到 BGP。

**原理**:
```
Node-1 上运行的容器:
  - Pod-A: 10.244.1.5/32
  - Pod-B: 10.244.1.6/32
  - Pod-C: 10.244.1.7/32

Node-1 的 BGP 进程发布:
  - 10.244.1.5/32 via Node-1 (下一跳: Node-1 的 Loopback IP)
  - 10.244.1.6/32 via Node-1
  - 10.244.1.7/32 via Node-1
```

### 4.2 路由传播路径

```
Container IP (10.244.1.5/32) 路由传播:

1. [Node-1]
   ↓ (BGP Announce)
   本地路由表添加: 10.244.1.5/32 dev cali1a2b3c4d scope link
   ↓
   通过 BGP 发布到 Leaf-1 (eBGP: AS 4200000001 → AS 65001)

2. [Leaf-1]
   ↓ (BGP Receive)
   接收路由: 10.244.1.5/32 via Node-1-IP (next-hop: 192.168.1.10)
   ↓
   发布到 Spine-1/Spine-2 (eBGP: AS 65001 → AS 65000)

3. [Spine-1/2]
   ↓ (BGP Receive + Reflect)
   接收路由: 10.244.1.5/32 via Leaf-1-IP
   ↓
   反射到所有其他 Leaf (eBGP: AS 65000 → AS 65002/65003...)

4. [Leaf-2/3/N]
   ↓ (BGP Receive)
   接收路由: 10.244.1.5/32 via Spine-1/2-IP (ECMP)
   ↓
   发布到下联的所有 Node (eBGP)

5. [Node-2/3/N]
   ↓ (BGP Receive)
   接收路由: 10.244.1.5/32 via Leaf-X-IP
   ↓
   安装到本地路由表，可达 10.244.1.5
```

### 4.3 路由汇总 (可选优化)

**问题**:
- 每个容器一条 /32 路由 → 10万 Pod = 10万条路由
- Spine 路由表过大，影响性能

**解决方案**: 在 Leaf 层汇总路由

```
Leaf-1 汇总规则:
  - 接收: Node-1/2/3 的所有 /32 路由 (10.244.1.0/24 范围)
  - 发布: 仅发布汇总路由 10.244.1.0/24 到 Spine

Spine 视角:
  - Leaf-1: 10.244.1.0/24
  - Leaf-2: 10.244.2.0/24
  - Leaf-3: 10.244.3.0/24
  总路由数: 仅几百条（而非几十万）
```

**注意**:
- 汇总仅在 Leaf→Spine 方向
- Leaf→Node 方向仍发布精确 /32 路由（保证 Node 间互通）

---

## 5. 数据包转发路径

### 5.1 Pod-to-Pod 通信（同节点）

```
Container-A (10.244.1.5) → Container-B (10.244.1.6)
  └─ 同在 Node-1

路径:
  Container-A
    ↓ (veth pair)
  cali1a2b3c4d (Node-1 网卡)
    ↓ (本地路由表: 10.244.1.6 dev cali5e6f7g8h)
  cali5e6f7g8h (Node-1 网卡)
    ↓ (veth pair)
  Container-B

延迟: < 0.1ms (纯本地转发)
```

### 5.2 Pod-to-Pod 通信（跨节点，同 Leaf）

```
Container-A (10.244.1.5 @ Node-1) → Container-C (10.244.2.7 @ Node-3)
  └─ Node-1 和 Node-3 都连接到 Leaf-1

数据包流向:

1. [Container-A → Node-1]
   源: 10.244.1.5
   目标: 10.244.2.7
   ↓
   查路由表: 10.244.2.7/32 via 192.168.1.20 (Leaf-1 IP)

2. [Node-1 → Leaf-1]
   ↓ (Ethernet frame)
   目标 MAC: Leaf-1 MAC
   目标 IP: 10.244.2.7

3. [Leaf-1 → Node-3]
   查路由表: 10.244.2.7/32 via 192.168.1.30 (Node-3 IP)
   ↓
   转发到 Node-3

4. [Node-3 → Container-C]
   查路由表: 10.244.2.7 dev cali9h8i7j6k
   ↓
   通过 veth pair 送达 Container-C

延迟: ~0.5ms (一跳交换机)
```

### 5.3 Pod-to-Pod 通信（跨节点，跨 Leaf）

```
Container-A (10.244.1.5 @ Node-1 @ Leaf-1)
  → Container-D (10.244.3.9 @ Node-5 @ Leaf-3)

数据包流向:

1. [Container-A → Node-1]
   查路由表: 10.244.3.9/32 via 192.168.1.10 (Leaf-1 IP)

2. [Node-1 → Leaf-1]
   Leaf-1 查路由表:
     10.244.3.9/32 via 10.0.1.1 (Spine-1)
     10.244.3.9/32 via 10.0.1.2 (Spine-2)
   ↓
   ECMP 选择: Spine-1 (负载均衡)

3. [Leaf-1 → Spine-1]
   Spine-1 查路由表:
     10.244.3.9/32 via 10.0.2.3 (Leaf-3)

4. [Spine-1 → Leaf-3]
   Leaf-3 查路由表:
     10.244.3.9/32 via 192.168.3.50 (Node-5)

5. [Leaf-3 → Node-5]
   Node-5 查路由表:
     10.244.3.9 dev cali1k2l3m4n

6. [Node-5 → Container-D]
   通过 veth pair 送达

延迟: ~1.5ms (三跳: Leaf → Spine → Leaf)
```

### 5.4 数据包封装

**重要**: 本方案使用**纯三层路由**，无封装！

```
对比:

Overlay (VXLAN):
  [Outer IP][Outer UDP][VXLAN Header][Inner Ethernet][Inner IP][Payload]
  ↑ 50 字节额外开销

Underlay (BGP):
  [Ethernet][IP][Payload]
  ↑ 无额外开销！

优势:
  - MTU 无损失 (1500)
  - CPU 开销低 (无封装/解封装)
  - 网络设备可直接查看容器 IP (易于排查)
```

---

## 6. 节点 BGP 配置

### 6.1 节点上的 BGP 服务

**选择**: 运行 BGP daemon (推荐方案: BIRD 或 GoBGP)

#### **方案对比**

| BGP 实现 | 优势 | 劣势 | 推荐度 |
|---------|------|------|--------|
| **BIRD** | 成熟稳定，性能高，配置灵活 | 配置语法复杂 | ⭐⭐⭐⭐⭐ |
| **GoBGP** | Go 编写，易集成，API 友好 | 性能略低于 BIRD | ⭐⭐⭐⭐ |
| **FRR** | 全功能路由套件 | 占用资源多 | ⭐⭐⭐ |
| **ExaBGP** | Python 编写，易定制 | 性能较差 | ⭐⭐ |

**推荐**: **BIRD 2.0+** (Calico 默认选择)

### 6.2 节点 BGP 配置示例 (BIRD)

#### **Node-1 配置** (/etc/bird/bird.conf)

```bash
# Router ID = Node IP
router id 192.168.1.10;

# 日志配置
log syslog all;
debug protocols { states, routes };

# 定义协议
protocol device {
  scan time 10;
}

protocol direct {
  ipv4;
  interface "cali*";  # 监听所有 Calico 接口
}

protocol kernel {
  ipv4 {
    export all;  # 导出所有 BGP 学到的路由到内核
    import none; # 不从内核导入路由
  };
  learn;
  merge paths on;  # 启用 ECMP
}

# 定义 BGP 模板
template bgp bgp_template {
  local as 4200000001;  # 本节点 AS 号
  multihop;
  hold time 90;
  keepalive time 30;
  connect retry time 5;
  error wait time 5,30;

  ipv4 {
    import all;  # 接收所有路由
    export where proto = "direct";  # 仅导出容器路由
    next hop self;
    add paths tx;  # 支持 ECMP
  };

  # 启用 BFD (可选，快速故障检测)
  bfd on;
}

# BGP Peering to Leaf-1
protocol bgp leaf1 from bgp_template {
  neighbor 192.168.1.1 as 65001;  # Leaf-1 IP 和 AS 号
  description "BGP to Leaf-1";
}

# BGP Peering to Leaf-2 (冗余)
protocol bgp leaf2 from bgp_template {
  neighbor 192.168.1.2 as 65001;  # Leaf-2 IP 和 AS 号
  description "BGP to Leaf-2";
}

# 过滤器: 只发布容器 IP
filter export_container_routes {
  if net ~ 10.244.0.0/16 && net.len = 32 then {
    accept;
  }
  reject;
}

protocol bgp leaf1 from bgp_template {
  neighbor 192.168.1.1 as 65001;
  ipv4 {
    export filter export_container_routes;  # 应用过滤器
  };
}
```

### 6.3 路由注入机制

**如何将容器 IP 注入 BGP？**

**方案 1**: Calico 自动管理 (推荐)
```
Calico Felix → 监听 Kubernetes Pod 创建
  ↓
创建 veth pair: cali1a2b3c4d ↔ Container eth0
  ↓
添加路由到内核:
  ip route add 10.244.1.5/32 dev cali1a2b3c4d scope link
  ↓
BIRD 从 "direct" 协议读取该路由
  ↓
通过 BGP 发布到 Leaf
```

**方案 2**: 手动管理 (用于理解)
```bash
# 1. 创建容器网卡
ip link add cali1 type veth peer name eth0

# 2. 将 eth0 移入容器命名空间
ip link set eth0 netns <container_ns>
ip netns exec <container_ns> ip addr add 10.244.1.5/32 dev eth0
ip netns exec <container_ns> ip link set eth0 up

# 3. 配置主机侧
ip link set cali1 up
ip route add 10.244.1.5/32 dev cali1 scope link

# 4. BIRD 自动检测到新路由并发布 (通过 protocol direct)
```

### 6.4 iBGP vs eBGP 决策

**问题**: 节点到 Leaf 应该用 iBGP 还是 eBGP？

| 方案 | AS 配置 | 优势 | 劣势 |
|------|---------|------|------|
| **eBGP** | 每节点独立 AS | ✅ 无 full mesh<br>✅ 快速收敛<br>✅ 扩展性好 | AS 号管理 |
| **iBGP** | 所有节点同一 AS | ✅ AS 号简单 | ❌ 需要 RR 或 full mesh<br>❌ 收敛慢 |

**结论**: **使用 eBGP** ✅

原因:
1. **扩展性**: 数千节点无需 full mesh
2. **收敛速度**: eBGP 直接传播，无需 RR 反射
3. **故障隔离**: 每节点独立 AS，故障域隔离
4. **业界实践**: Calico、Cilium、kube-router 均采用 eBGP

---

## 7. 与 IPAM 集成

### 7.1 IP 分配流程

```
1. [Kubernetes API Server]
   创建 Pod: nginx-pod
   ↓
2. [Kubelet]
   调用 CNI ADD 命令
   ↓
3. [CNI Plugin (Calico/本方案)]
   调用 IPAM gRPC API: AllocateIP(node_id="node-1")
   ↓
4. [IPAM Daemon (本项目)]
   基于拓扑分配 IP: 10.244.1.5/32 (来自 TOR-1 的网段池)
   ↓
   返回: IP=10.244.1.5, CIDR=10.244.1.5/32, Gateway=169.254.1.1
   ↓
5. [CNI Plugin]
   配置容器网卡: eth0 = 10.244.1.5/32
   配置主机路由: ip route add 10.244.1.5/32 dev cali1a2b3c4d
   ↓
6. [BIRD (BGP)]
   检测到新路由 (protocol direct)
   ↓
   通过 BGP 发布: 10.244.1.5/32 via Node-1
   ↓
7. [Leaf Switch]
   接收 BGP 路由
   ↓
   安装到路由表: 10.244.1.5/32 via 192.168.1.10 (Node-1)
```

### 7.2 拓扑感知 IP 分配与 BGP 的结合

**设计思路**: IPAM 的拓扑结构与 BGP 网络拓扑**对齐**

```
IPAM 拓扑:
  Zone-A → Pod-1 → TOR-1 → [Node-1, Node-2, Node-3]
                                ↓
                        Subnet Pool: 10.244.1.0/24

BGP 拓扑:
  Spine-1/2 → Leaf-1 → TOR-1 → [Node-1, Node-2, Node-3]
                                     ↓
                         BGP 发布: 10.244.1.0/24 的 /32 路由
```

**优势**:
1. **网段收敛**: 同一 TOR 下的节点共享网段池 → BGP 可在 Leaf 汇总
2. **故障域隔离**: TOR 故障仅影响该 TOR 下的节点
3. **流量优化**: 同 TOR 下的 Pod 通信延迟最低

### 7.3 IPAM 配置示例

```json
{
  "zones": [{
    "id": "zone-a",
    "name": "Beijing Zone A",
    "pods": [{
      "id": "pod-1",
      "name": "Pod 1 - Rack Row A",
      "tors": [{
        "id": "tor-1",
        "name": "TOR-R01-A",
        "location": "Rack 01",
        "subnets": [
          {
            "cidr": "10.244.1.0/24",
            "purpose": "default",
            "gateway": "169.254.1.1"
          },
          {
            "cidr": "10.244.100.0/24",
            "purpose": "storage",
            "gateway": "169.254.100.1"
          }
        ],
        "leaf_peers": [
          {"leaf_id": "leaf-1", "leaf_ip": "192.168.1.1", "leaf_as": 65001}
        ]
      }]
    }]
  }]
}
```

---

## 8. 实现方案

### 8.1 技术栈

| 组件 | 技术选型 | 说明 |
|------|---------|------|
| **IPAM** | 本项目 (Go) | 拓扑感知 IP 分配 |
| **BGP Daemon** | BIRD 2.0+ | 节点 BGP 路由 |
| **CNI Plugin** | 本项目 + Calico | 容器网络配置 |
| **路由注入** | BIRD Direct Protocol | 自动检测内核路由 |
| **交换机** | Arista/Cisco/Cumulus | 支持 BGP 的 Leaf/Spine |

### 8.2 部署架构

```
每个节点运行:
  ┌─────────────────────────────────┐
  │  Kubernetes Node                │
  │                                 │
  │  ┌──────────┐   ┌─────────┐   │
  │  │  BIRD    │←→│  Kernel │   │
  │  │  (BGP)   │   │ Routing │   │
  │  └────┬─────┘   └────┬────┘   │
  │       │ BGP           │        │
  │       │ Peering       │ Routes │
  │       ↓               ↓        │
  │  ┌──────────────────────────┐ │
  │  │   CNI Plugin (Calico)    │ │
  │  └──────────┬───────────────┘ │
  │             │ gRPC             │
  │  ┌──────────▼───────────────┐ │
  │  │   IPAM Daemon (本项目)   │ │
  │  │   - Topology Aware       │ │
  │  │   - Raft Consensus       │ │
  │  └──────────────────────────┘ │
  │                                 │
  │  [Pod 1] [Pod 2] ... [Pod N]   │
  └─────────────────────────────────┘
        │
        ↓ BGP (eBGP)
   [Leaf Switch]
```

### 8.3 配置文件示例

#### **/etc/calico/calico.yaml**

```yaml
apiVersion: projectcalico.org/v3
kind: BGPConfiguration
metadata:
  name: default
spec:
  logSeverityScreen: Info
  nodeToNodeMeshEnabled: false  # 禁用 node-to-node mesh
  asNumber: 0  # 每个节点使用独立 AS (从 node resource 读取)
  serviceClusterIPs:
  - cidr: 10.96.0.0/12

---
apiVersion: projectcalico.org/v3
kind: Node
metadata:
  name: node-1
spec:
  bgp:
    asNumber: 4200000001  # 节点独立 AS
    ipv4Address: 192.168.1.10/24
  orchRefs:
  - nodeName: node-1

---
apiVersion: projectcalico.org/v3
kind: BGPPeer
metadata:
  name: leaf-1-peer
spec:
  node: node-1
  peerIP: 192.168.1.1  # Leaf-1 IP
  asNumber: 65001       # Leaf-1 AS
  keepOriginalNextHop: false
```

### 8.4 Leaf 交换机配置示例 (Arista EOS)

```bash
# Leaf-1 配置

! BGP 基础配置
router bgp 65001
  router-id 192.168.1.1
  maximum-paths 32  # 启用 ECMP

  ! BGP Peering to Spine
  neighbor SPINE peer group
  neighbor SPINE remote-as 65000
  neighbor SPINE maximum-routes 50000
  neighbor SPINE send-community
  neighbor 10.0.1.1 peer group SPINE  # Spine-1
  neighbor 10.0.1.2 peer group SPINE  # Spine-2

  ! BGP Peering to Nodes (动态邻居)
  bgp listen range 192.168.1.0/24 peer-group NODES remote-as 4200000000-4200099999
  neighbor NODES maximum-routes 10000
  neighbor NODES route-map NODE_IN in
  neighbor NODES route-map NODE_OUT out

  ! 地址族配置
  address-family ipv4
    neighbor SPINE activate
    neighbor SPINE next-hop-self
    neighbor NODES activate
    network 10.244.0.0/16  # 容器 CIDR

! 路由策略
route-map NODE_IN permit 10
  match ip address prefix-list CONTAINER_IPS

route-map NODE_OUT permit 10
  match ip address prefix-list ALL_ROUTES

ip prefix-list CONTAINER_IPS seq 10 permit 10.244.0.0/16 le 32
ip prefix-list ALL_ROUTES seq 10 permit 0.0.0.0/0 le 32

! BFD 配置 (可选)
bfd interval 300 min-rx 300 multiplier 3
```

### 8.5 Spine 交换机配置示例

```bash
# Spine-1 配置

router bgp 65000
  router-id 10.0.1.1
  maximum-paths 32

  ! BGP Peering to all Leafs
  neighbor LEAF peer group
  neighbor LEAF send-community
  neighbor LEAF route-reflector-client  # 作为 RR (可选)
  neighbor LEAF maximum-routes 100000

  neighbor 192.168.1.1 peer group LEAF
  neighbor 192.168.1.1 remote-as 65001  # Leaf-1

  neighbor 192.168.2.1 peer group LEAF
  neighbor 192.168.2.1 remote-as 65002  # Leaf-2

  # ... 更多 Leaf

  address-family ipv4
    neighbor LEAF activate
    neighbor LEAF next-hop-unchanged  # 保持原始下一跳 (RR 模式)
```

---

## 9. 数据流总结

### 9.1 控制平面（路由传播）

```
Pod 创建 (10.244.1.5)
  ↓
[IPAM] 分配 IP (基于拓扑)
  ↓
[CNI Plugin] 配置网卡 + 添加路由
  ↓
[Kernel] 路由表: 10.244.1.5/32 dev cali1
  ↓
[BIRD] 检测路由 (protocol direct)
  ↓
[BIRD] BGP Announce to Leaf (eBGP)
  ↓
[Leaf] 接收路由 + 安装到 RIB
  ↓
[Leaf] BGP Announce to Spine (eBGP)
  ↓
[Spine] 接收路由 + 反射到其他 Leaf (eBGP)
  ↓
[Other Leafs] 接收路由 + BGP Announce to Nodes
  ↓
[Other Nodes] 接收路由 + 安装到内核
  ↓
✅ 全网可达 10.244.1.5/32
```

### 9.2 数据平面（数据包转发）

```
Pod-A (10.244.1.5 @ Node-1) → Pod-B (10.244.3.10 @ Node-5)

[Pod-A]
  ↓ ARP: Gateway 169.254.1.1 (特殊地址)
  ↓ 实际下一跳: Node-1 路由栈
[Node-1 Kernel]
  ↓ 查路由表: 10.244.3.10/32 via 192.168.1.1 (Leaf-1)
[Leaf-1]
  ↓ 查路由表: 10.244.3.10/32 via 10.0.1.1 (Spine-1, ECMP)
[Spine-1]
  ↓ 查路由表: 10.244.3.10/32 via 192.168.3.1 (Leaf-3)
[Leaf-3]
  ↓ 查路由表: 10.244.3.10/32 via 192.168.3.50 (Node-5)
[Node-5 Kernel]
  ↓ 查路由表: 10.244.3.10 dev cali9h8i7
[Pod-B]
  ✅ 接收数据包
```

---

## 10. 优势与挑战

### 10.1 优势

1. **性能**
   - 纯三层路由，无 overlay 开销
   - ECMP 负载均衡，充分利用多路径
   - 延迟低：同机架 < 1ms，跨机架 < 2ms

2. **可扩展性**
   - 支持上万节点（eBGP 架构）
   - 路由汇总降低路由表规模
   - 水平扩展 Spine/Leaf

3. **可靠性**
   - BGP 快速收敛（< 1s）
   - BFD 快速故障检测（< 300ms）
   - 多路径冗余（ECMP）

4. **可见性**
   - 网络设备可直接看到容器 IP
   - 传统网络工具可用（traceroute、tcpdump）
   - 易于调试和排查

### 10.2 挑战

1. **网络设备要求**
   - 需要支持 BGP 的交换机
   - 路由表容量要求高（10万+ 路由）

2. **配置复杂度**
   - BGP 配置较复杂
   - AS 号管理
   - 路由策略设计

3. **IP 地址消耗**
   - 每个容器需要独立 /32 IP
   - 需要足够大的 IP 地址段

---

## 11. 下一步实现

### 11.1 Phase 1: 基础集成
- [ ] 实现 CNI 插件与 IPAM 集成
- [ ] 节点 BIRD 配置模板
- [ ] 自动化 BGP peering 配置
- [ ] 路由注入测试

### 11.2 Phase 2: 拓扑对齐
- [ ] IPAM 拓扑配置中添加 Leaf/Spine 信息
- [ ] 自动生成 Leaf 汇总路由
- [ ] 拓扑感知的 BGP peer 管理

### 11.3 Phase 3: 高级特性
- [ ] BGP 路由策略优化
- [ ] BFD 快速收敛
- [ ] 多路径 ECMP 验证
- [ ] 监控和告警集成

---

## 参考资料

- [RFC 4271](https://datatracker.ietf.org/doc/html/rfc4271) - BGP-4
- [RFC 7938](https://datatracker.ietf.org/doc/html/rfc7938) - Use of BGP for Routing in Large-Scale Data Centers
- [Calico BGP Architecture](https://docs.tigera.io/calico/latest/reference/architecture/overview)
- [BIRD Internet Routing Daemon](https://bird.network.cz/)
- [Leaf-Spine Architecture](https://www.cisco.com/c/en/us/products/collateral/switches/nexus-9000-series-switches/white-paper-c11-737022.html)
