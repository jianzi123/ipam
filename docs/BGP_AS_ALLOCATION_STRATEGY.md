# BGP AS 号分配策略分析

## 核心问题

在 Kubernetes CNI 的 BGP 网络架构中，如何分配 AS 号？

**可选方案**：
1. 一个节点一个 AS（Node-level）
2. 一个 TOR 对应的所有节点一个 AS（TOR-level）
3. 一个 Leaf 对应的所有节点一个 AS（Leaf-level）
4. 一个 Spine 对应的所有节点一个 AS（Spine-level）
5. 所有节点同一个 AS（Cluster-level）

---

## 方案对比表

| 方案 | BGP类型 | 扩展性 | 故障隔离 | 配置复杂度 | 推荐度 |
|------|---------|--------|----------|------------|--------|
| **1节点1AS** | eBGP | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | **✅ 强烈推荐** |
| **1TOR多节点1AS** | 混合 | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ | ✅ 推荐 |
| **1Leaf多节点1AS** | 混合 | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐ | ⚠️ 谨慎 |
| **1Spine多节点1AS** | 混合 | ⭐⭐ | ⭐⭐ | ⭐ | ❌ 不推荐 |
| **全部节点1AS** | iBGP | ⭐ | ⭐ | ⭐ | ❌ 不推荐 |

---

## 方案 1: 一个节点一个 AS ✅ **强烈推荐**

### 架构图

```
Spine (AS 64512)
  ├─ Leaf-1 (AS 65001)
  │   ├─ TOR-1 (AS 65101)
  │   │   ├─ Node-1 (AS 4200000001) ← 独立 AS
  │   │   ├─ Node-2 (AS 4200000002) ← 独立 AS
  │   │   └─ Node-3 (AS 4200000003) ← 独立 AS
  │   │
  │   └─ TOR-2 (AS 65102)
  │       ├─ Node-4 (AS 4200000004)
  │       └─ Node-5 (AS 4200000005)
  │
  └─ Leaf-2 (AS 65002)
      └─ TOR-3 (AS 65103)
          ├─ Node-6 (AS 4200000006)
          └─ Node-7 (AS 4200000007)
```

### BGP 对等关系

```
全部使用 eBGP！

Node-1 (AS 4200000001)
  ↕ eBGP
TOR-1 (AS 65101)
  ↕ eBGP
Leaf-1 (AS 65001)
  ↕ eBGP
Spine (AS 64512)
```

### 优势

#### 1. **最佳扩展性**（⭐⭐⭐⭐⭐）

```
每个节点只需连接 1-2 个上游设备（TOR 或 Leaf）

连接数:
- Node 层: N 节点 × 2 连接 = 2N 条连接
- TOR 层: M 个 TOR × 2 Leaf = 2M 条连接
- Leaf 层: L 个 Leaf × 2 Spine = 2L 条连接

总计: O(N) 线性扩展 ✅

对比 iBGP Full Mesh: O(N²) ❌
```

#### 2. **最强故障隔离**（⭐⭐⭐⭐⭐）

```
Node-1 的 BGP 配置错误只影响 Node-1

故障影响范围:
- Node-1 宕机 → 只影响 Node-1 上的容器 ✅
- TOR-1 宕机 → 只影响 TOR-1 下的节点 ✅
- Leaf-1 宕机 → ECMP 自动切换到 Leaf-2 ✅

无级联故障！
```

#### 3. **自动路由传播**（⭐⭐⭐⭐⭐）

```
eBGP 特性:
- Next-Hop 自动更新 ✅
- 路由自动传播给所有 eBGP 邻居 ✅
- AS Path 自动添加（防环） ✅
- 无需 Route Reflector ✅
```

#### 4. **快速收敛**（⭐⭐⭐⭐⭐）

```bash
# FRR 配置
router bgp 4200000001
  timers bgp 3 9           # Keepalive 3s, Hold 9s
  neighbor TOR bfd         # BFD 亚秒级检测

故障检测: < 1 秒（BFD）
路由收敛: < 3 秒（BGP）
```

### 劣势

#### 1. **AS 号消耗多**

```
需要 N 个 AS 号（N = 节点数）

解决方案: 使用 4 字节私有 AS
- 范围: 4200000000 - 4294967294
- 总数: 94,967,294 个 AS 号
- 足够支持 9400 万个节点！✅
```

#### 2. **配置分散**

```
每个节点需要独立配置 AS 号

解决方案: 自动化配置
- Ansible/SaltStack 模板化
- 或通过 IPAM 系统动态生成 FRR 配置
```

### 配置示例

```bash
# Node-1 (AS 4200000001)
router bgp 4200000001
  neighbor 192.168.1.1 remote-as 65101    # TOR-1

# Node-2 (AS 4200000002)
router bgp 4200000002
  neighbor 192.168.1.1 remote-as 65101    # TOR-1

# TOR-1 (AS 65101)
router bgp 65101
  neighbor 192.168.1.10 remote-as 4200000001  # Node-1
  neighbor 192.168.1.11 remote-as 4200000002  # Node-2
  neighbor 192.168.2.1 remote-as 65001        # Leaf-1
```

### 业界实践

- **Calico**: 默认每节点一个 AS（Node-to-Node Mesh 模式除外）
- **Cilium**: BGP Control Plane 模式下每节点独立 AS
- **Cumulus Linux**: 推荐 eBGP Unnumbered，每节点独立 AS
- **Facebook**: 数据中心网络使用 eBGP，每服务器独立 AS

---

## 方案 2: 一个 TOR 对应的所有节点一个 AS ✅ **推荐**

### 架构图

```
Spine (AS 64512)
  ├─ Leaf-1 (AS 65001)
  │   ├─ TOR-1 (AS 65101)  ← 同一 AS 边界
  │   │   ├─ Node-1 (AS 65101) ← 同 TOR AS
  │   │   ├─ Node-2 (AS 65101) ← 同 TOR AS (iBGP)
  │   │   └─ Node-3 (AS 65101) ← 同 TOR AS (iBGP)
  │   │
  │   └─ TOR-2 (AS 65102)
  │       ├─ Node-4 (AS 65102)
  │       └─ Node-5 (AS 65102)
  │
  └─ Leaf-2 (AS 65002)
      └─ TOR-3 (AS 65103)
          ├─ Node-6 (AS 65103)
          └─ Node-7 (AS 65103)
```

### BGP 对等关系

```
TOR 内节点: iBGP
TOR 之间: eBGP

Node-1 (AS 65101)
  ↕ iBGP (需要 Full Mesh 或 RR)
Node-2 (AS 65101)
  ↕ iBGP
Node-3 (AS 65101)
  ↕ iBGP (到 TOR-1)
TOR-1 (AS 65101, 作为 Route Reflector)
  ↕ eBGP
Leaf-1 (AS 65001)
```

### 优势

#### 1. **对齐网络拓扑**（⭐⭐⭐⭐⭐）

```
AS 边界 = TOR 物理边界

- 逻辑与物理拓扑一致 ✅
- 符合传统数据中心网络设计 ✅
- 便于故障定位（AS = 故障域） ✅
```

#### 2. **减少 AS 号消耗**（⭐⭐⭐⭐）

```
AS 号需求: TOR 数量（而非节点数量）

示例:
- 100 个 TOR × 40 节点/TOR = 4000 节点
- 只需 100 个 AS 号 ✅

对比方案1: 需要 4000 个 AS 号
```

#### 3. **路由聚合机会**（⭐⭐⭐⭐）

```
TOR 层可以聚合路由

TOR-1 管理 10.244.1.0/24:
- Node-1: 10.244.1.1-50   (50 个 /32)
- Node-2: 10.244.1.51-100 (50 个 /32)
- Node-3: 10.244.1.101-150 (50 个 /32)

向 Leaf 发布:
- 选项 A: 150 个 /32 路由（精确）
- 选项 B: 1 个 /24 路由（聚合）✅ 减少路由表

但注意: 聚合会降低故障收敛速度
```

### 劣势

#### 1. **TOR 内需要 iBGP Full Mesh 或 Route Reflector**（⭐⭐）

```
场景: TOR-1 下 40 个节点

方案 A: iBGP Full Mesh
- 连接数: 40 × 39 / 2 = 780 条 iBGP 连接 ❌
- 不可行！

方案 B: TOR 作为 Route Reflector ✅
- 每个节点只连 TOR
- TOR 成为单点故障 ⚠️
- 需要双 TOR 冗余 + VRRP
```

#### 2. **Next-Hop 需要手动配置**（⭐⭐）

```bash
# 节点配置（iBGP）
router bgp 65101
  neighbor TOR_RR peer-group
  neighbor TOR_RR remote-as 65101      # 同 AS = iBGP

  address-family ipv4 unicast
    neighbor TOR_RR next-hop-self      # ← 必须手动配置！
    neighbor TOR_RR route-reflector-client
  exit-address-family

# 如果忘记配置 next-hop-self
# → 路由的 Next-Hop 保持为原始 Node IP
# → Leaf 无法到达 → 路由黑洞！❌
```

#### 3. **TOR 成为瓶颈**（⭐⭐⭐）

```
所有节点的 BGP 路由都经过 TOR Route Reflector

- TOR CPU/内存负载高 ⚠️
- TOR 故障影响整个 TOR 下的节点 ⚠️
- 需要双 TOR 冗余 + VRRP/MLAG（复杂）
```

### 配置示例

```bash
# ============================================
# TOR-1 配置 (Route Reflector)
# ============================================
router bgp 65101
  bgp router-id 192.168.1.1
  bgp cluster-id 192.168.1.1           # RR Cluster ID

  # iBGP 到节点（作为 RR 服务器）
  neighbor NODES peer-group
  neighbor NODES remote-as 65101       # 同 AS = iBGP

  neighbor 192.168.1.10 peer-group NODES  # Node-1
  neighbor 192.168.1.10 route-reflector-client

  neighbor 192.168.1.11 peer-group NODES  # Node-2
  neighbor 192.168.1.11 route-reflector-client

  # eBGP 到 Leaf
  neighbor 192.168.2.1 remote-as 65001    # Leaf-1

  address-family ipv4 unicast
    neighbor NODES next-hop-self         # 修改 Next-Hop!
    neighbor NODES activate
    neighbor 192.168.2.1 activate
  exit-address-family

# ============================================
# Node-1 配置 (RR Client)
# ============================================
router bgp 65101
  bgp router-id 192.168.1.10

  # iBGP 到 TOR (Route Reflector)
  neighbor 192.168.1.1 remote-as 65101    # 同 AS = iBGP
  neighbor 192.168.1.1 description "TOR-1 RR"

  address-family ipv4 unicast
    redistribute connected route-map CONTAINER_IPS
    neighbor 192.168.1.1 activate
  exit-address-family
```

### 适用场景

✅ **适合**：
- TOR 设备性能强（支持 Route Reflector 负载）
- 每个 TOR 下节点数适中（< 50 节点）
- 需要路由聚合以减少路由表规模
- 网络拓扑高度结构化（TOR 边界清晰）

❌ **不适合**：
- TOR 设备老旧/性能弱
- 每个 TOR 下节点数很多（> 100 节点）
- 没有双 TOR 冗余机制

---

## 方案 3: 一个 Leaf 对应的所有节点一个 AS ⚠️ **谨慎**

### 架构图

```
Spine (AS 64512)
  ├─ Leaf-1 (AS 65001)  ← 同一 AS 边界
  │   ├─ TOR-1 (AS 65001)
  │   │   ├─ Node-1 (AS 65001) ← iBGP
  │   │   ├─ Node-2 (AS 65001) ← iBGP
  │   │   └─ Node-3 (AS 65001) ← iBGP
  │   │
  │   └─ TOR-2 (AS 65001)
  │       ├─ Node-4 (AS 65001) ← iBGP
  │       └─ Node-5 (AS 65001) ← iBGP
  │
  └─ Leaf-2 (AS 65002)
      └─ ...
```

### BGP 对等关系

```
Leaf 内所有设备: iBGP (需要 Full Mesh 或多级 RR)
Leaf 之间: eBGP

Node-1 (AS 65001)
  ↕ iBGP
TOR-1 (AS 65001, RR)
  ↕ iBGP
Leaf-1 (AS 65001, Super RR)
  ↕ eBGP
Spine (AS 64512)
```

### 优势

#### 1. **AS 号消耗最少**（⭐⭐⭐⭐⭐）

```
AS 号需求: Leaf 数量（通常 < 10）

示例:
- 4 个 Leaf × 1000 节点/Leaf = 4000 节点
- 只需 4 个 AS 号！✅
```

#### 2. **大规模路由聚合**（⭐⭐⭐⭐）

```
Leaf-1 管理 10.244.0.0/16:
- TOR-1: 10.244.1.0/24
- TOR-2: 10.244.2.0/24
- ...

向 Spine 发布:
- 1 个 /16 聚合路由 ✅
- Spine 路由表极小
```

### 劣势

#### 1. **需要多级 Route Reflector**（⭐）

```
层级架构:

Leaf-1 (Super RR)
  ├─ TOR-1 (Sub RR)
  │   ├─ Node-1 (RR Client)
  │   ├─ Node-2 (RR Client)
  │   └─ ...
  │
  ├─ TOR-2 (Sub RR)
  │   └─ ...
  └─ ...

配置复杂度极高！❌
```

#### 2. **路由传播路径长**（⭐⭐）

```
Node-1 发布路由:
Node-1 → TOR-1 (iBGP) → Leaf-1 (iBGP) → Spine (eBGP) → Leaf-2 (eBGP) → TOR-3 (iBGP) → Node-6 (iBGP)

- 6 跳 BGP 传播 ❌
- 收敛慢（可能 > 10 秒）
- 中间任何环节故障都影响全局
```

#### 3. **故障影响范围大**（⭐）

```
Leaf-1 故障:
- 影响下面所有 TOR 和节点 ❌
- 可能影响数百甚至上千个节点
- 恢复时间长
```

### 配置示例（过于复杂，不推荐）

```bash
# Leaf-1 (Super Route Reflector)
router bgp 65001
  bgp cluster-id 10.0.0.1

  # iBGP 到 TOR (Sub RR)
  neighbor 192.168.1.1 remote-as 65001
  neighbor 192.168.1.1 route-reflector-client

  # eBGP 到 Spine
  neighbor 10.0.1.1 remote-as 64512

# TOR-1 (Sub Route Reflector)
router bgp 65001
  bgp cluster-id 10.0.1.1

  # iBGP 到 Leaf (Super RR)
  neighbor 192.168.2.1 remote-as 65001

  # iBGP 到节点
  neighbor 192.168.1.10 remote-as 65001
  neighbor 192.168.1.10 route-reflector-client

# Node-1 (Double RR Client!)
router bgp 65001
  # iBGP 到 TOR
  neighbor 192.168.1.1 remote-as 65001
```

### 适用场景

⚠️ **仅在极端情况下考虑**：
- 路由表规模是绝对瓶颈（Spine 只能存几百条路由）
- 网络运维团队有丰富 iBGP RR 经验
- 可以接受复杂配置和慢收敛

❌ **大多数情况不推荐**

---

## 方案 4: 一个 Spine 对应的节点一个 AS ❌ **不推荐**

类似方案 3，但范围更大，问题更严重：

```
- iBGP Full Mesh 规模: 整个 Spine 域（可能数千节点）❌
- 需要三级或四级 Route Reflector ❌
- 故障影响范围: 整个 Spine 域 ❌
- 配置复杂度: 极高 ❌
```

**结论**: 完全不推荐！

---

## 方案 5: 所有节点同一个 AS ❌ **不推荐**

### 架构

```
整个集群一个 AS (例如 AS 65000)

Spine (AS 65000)
  ├─ Leaf-1 (AS 65000) ← 全部 iBGP!
  │   ├─ TOR-1 (AS 65000)
  │   │   ├─ Node-1 (AS 65000)
  │   │   └─ ...
  │   └─ ...
  └─ ...
```

### 严重问题

#### 1. **需要全网 Full Mesh 或复杂 RR 层级**（❌）

```
4000 个节点 Full Mesh:
- 连接数: 4000 × 3999 / 2 = 7,998,000 条连接
- 完全不可行！❌

必须用多级 RR:
- Super RR → Sub RR → Sub-Sub RR → ...
- 配置和维护噩梦 ❌
```

#### 2. **没有 AS Path 防环**（❌）

```
iBGP 不修改 AS Path
→ 无法通过 AS Path 检测环路
→ 必须依赖 BGP Cluster List（复杂）
→ 容易配置错误导致环路 ❌
```

#### 3. **Next-Hop 处理复杂**（❌）

```
每一级 RR 都需要配置 next-hop-self
→ 容易遗漏
→ 导致路由黑洞
```

**结论**: 完全不推荐！只适合极小规模测试环境（< 10 节点）

---

## 推荐策略总结

### 🏆 **首选方案: 一个节点一个 AS**

```
优势:
✅ 最佳扩展性（线性扩展到数万节点）
✅ 最强故障隔离（节点级隔离）
✅ 配置简单（全部 eBGP，无 RR）
✅ 快速收敛（< 3 秒）
✅ 业界标准（Calico、Cilium 默认）

劣势:
⚠️ AS 号消耗多（但 4 字节私有 AS 足够）
⚠️ 每节点配置独立（但可自动化）

适用场景: 99% 的生产环境 ✅
```

### 🥈 **次选方案: 一个 TOR 对应的节点一个 AS**

```
优势:
✅ 对齐物理拓扑
✅ 减少 AS 号消耗
✅ 可以路由聚合

劣势:
⚠️ 需要 TOR 作为 Route Reflector
⚠️ TOR 成为单点（需双 TOR 冗余）
⚠️ iBGP 配置复杂

适用场景:
- TOR 设备性能强
- 每 TOR 下节点数适中（< 50）
- 有双 TOR 冗余
```

### ⚠️ **特殊场景: 一个 Leaf 对应的节点一个 AS**

```
仅在以下情况考虑:
- 路由表规模是绝对瓶颈
- 网络团队有丰富 RR 经验
- 可以接受高复杂度和慢收敛

大多数情况不推荐
```

### ❌ **不推荐方案**

```
- 一个 Spine 对应的节点一个 AS ❌
- 所有节点同一个 AS ❌

原因: 扩展性差、配置复杂、故障影响大
```

---

## AS 号分配方案

### 方案 1 的 AS 号规划

```
组件层级              AS 号范围                数量
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Spine               64512 - 64520            1-10 个
Leaf                65000 - 65100            1-100 个
TOR                 65101 - 65999            100-1000 个
Node (4-byte)       4200000000 - 4294967294  数百万个
```

#### Node AS 号生成算法

```go
// pkg/topology/as_allocator.go

// 基于节点 ID 生成唯一 AS 号
func GenerateNodeAS(nodeID string) uint32 {
    // 基础 AS 号
    baseAS := uint32(4200000000)

    // 使用 hash 生成偏移
    hash := fnv.New32a()
    hash.Write([]byte(nodeID))
    offset := hash.Sum32() % 94967294  // 4 字节私有 AS 总数

    return baseAS + offset
}

// 或基于拓扑位置生成
func GenerateNodeASByTopology(zoneID, podID, torID string, nodeIndex int) uint32 {
    // Zone: 8 bits (256 个 Zone)
    // Pod: 8 bits (256 个 Pod/Zone)
    // TOR: 8 bits (256 个 TOR/Pod)
    // Node: 8 bits (256 个 Node/TOR)

    baseAS := uint32(4200000000)

    zoneNum := uint32(hashToInt(zoneID, 256))
    podNum := uint32(hashToInt(podID, 256))
    torNum := uint32(hashToInt(torID, 256))
    nodeNum := uint32(nodeIndex)

    // 编码: ZZPPTTNN
    return baseAS + (zoneNum << 24) + (podNum << 16) + (torNum << 8) + nodeNum
}
```

### 方案 2 的 AS 号规划

```
组件层级              AS 号范围                说明
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Spine               64512 - 64520            2 字节私有 AS
Leaf                65000 - 65100            2 字节私有 AS
TOR + 下属节点      65101 - 65999            共享 TOR 的 AS 号
```

---

## 迁移路径

### 从方案 5 (全iBGP) → 方案 1 (全eBGP)

```
阶段 1: 准备
- 为每个节点分配 AS 号
- 生成新的 FRR 配置文件

阶段 2: 灰度切换 (按 TOR 逐步迁移)
- TOR-1 下节点先切换到 eBGP
- 验证路由正确
- 逐步切换其他 TOR

阶段 3: 清理
- 移除 iBGP Route Reflector 配置
- 删除 iBGP neighbor 配置
```

---

## 实施建议

### 1. **新建集群**

✅ **直接使用方案 1**: 一个节点一个 AS（eBGP）

### 2. **已有集群（< 100 节点）**

考虑迁移到方案 1：
- 收益: 更好的扩展性和稳定性
- 成本: 一次性配置变更
- 风险: 可灰度迁移，风险可控

### 3. **已有集群（> 100 节点）**

评估现有方案:
- 如果用 iBGP + RR: 评估是否有扩展瓶颈
- 如果有瓶颈: 规划迁移到 eBGP
- 如果无瓶颈: 可暂时保持，新增节点用 eBGP

### 4. **超大规模集群（> 1000 节点）**

✅ **必须使用方案 1**: iBGP 无法满足扩展需求

---

## 常见问题 (FAQ)

### Q1: 每个节点独立 AS 号，会不会导致 AS Path 过长？

**A**: 不会。

```
从 Node-1 到 Node-2 的 AS Path:
Node-1 (4200000001) → TOR-1 (65101) → Leaf-1 (65001) → Spine (64512)
→ Leaf-2 (65002) → TOR-2 (65102) → Node-2 (4200000002)

AS Path 长度: 7
BGP 默认最大 AS Path: 255

7 << 255, 完全没问题 ✅
```

### Q2: 4 字节 AS 号兼容性如何？

**A**: 现代设备完全支持。

```
支持情况:
- FRR 7.0+: ✅ 完全支持
- BIRD 2.0+: ✅ 完全支持
- Cisco IOS-XE: ✅ 完全支持
- Juniper JunOS: ✅ 完全支持
- Cumulus Linux: ✅ 完全支持

老旧设备（2010 年前）: 可能不支持
解决方案: 升级设备固件或使用 2 字节 AS（受限）
```

### Q3: eBGP 比 iBGP 收敛更快吗？

**A**: 是的。

```
eBGP:
- 路由直接传播 ✅
- 无 RR 路径延迟 ✅
- 典型收敛时间: 1-3 秒

iBGP + RR:
- 路由需经过多级 RR ⚠️
- 每一级都有处理延迟 ⚠️
- 典型收敛时间: 5-15 秒

配合 BFD: eBGP 可实现亚秒级收敛（< 500ms）
```

### Q4: 我的 TOR 交换机不支持 BGP，怎么办？

**A**: 节点直接连 Leaf。

```
简化拓扑:
Leaf-1 (AS 65001)
  ├─ Node-1 (AS 4200000001) ← 直连 Leaf
  ├─ Node-2 (AS 4200000002)
  └─ Node-3 (AS 4200000003)

仍然使用方案 1 (每节点一个 AS)
只是跳过 TOR 层 ✅
```

### Q5: 可以混合使用不同方案吗？

**A**: 可以，但需要仔细规划。

```
示例: 混合架构
- 核心节点（高性能）: 方案 1 (每节点一个 AS)
- 边缘节点（低性能）: 方案 2 (TOR 级别 AS)

关键: AS 边界清晰，避免 iBGP/eBGP 混用导致问题
```

---

## 参考资料

### RFC 文档
- RFC 4271: BGP-4 Protocol
- RFC 6793: BGP Support for Four-Octet AS Number Space
- RFC 4456: BGP Route Reflection

### 最佳实践
- [Calico BGP Best Practices](https://docs.projectcalico.org/networking/bgp)
- [Cilium BGP Control Plane](https://docs.cilium.io/en/stable/network/bgp-control-plane/)
- [Cumulus Linux BGP](https://docs.nvidia.com/networking-ethernet-software/cumulus-linux/Layer-3/Border-Gateway-Protocol-BGP/)

### 本项目相关文档
- `BGP_NETWORK_DESIGN.md` - BGP 网络总体设计
- `BGP_IBGP_EBGP_ANALYSIS.md` - iBGP vs eBGP 深度分析
- `docs/BGP_AS_AND_SUBNET_RELATIONSHIP.md` - AS 号与网段关系
- `configs/frr-node-example.conf` - FRR 配置示例（方案 1）

---

## 总结

### 推荐决策树

```
开始
  ├─ 新建集群？
  │   └─ 是 → 【使用方案 1: 一节点一AS】✅
  │
  ├─ 已有集群规模？
  │   ├─ < 100 节点 → 考虑迁移到方案 1
  │   ├─ 100-1000 节点 → 评估后决定是否迁移
  │   └─ > 1000 节点 → 【必须迁移到方案 1】✅
  │
  ├─ 特殊约束？
  │   ├─ AS 号受限 → 考虑方案 2 (TOR 级别)
  │   ├─ 路由表受限 → 考虑方案 3 (Leaf 级别，谨慎)
  │   └─ TOR 不支持 BGP → 方案 1 但跳过 TOR 层
  │
  └─ 默认选择 → 【方案 1: 一节点一AS】✅
```

### 核心原则

1. **优先选择 eBGP** - 简单、可扩展、快速收敛
2. **避免大规模 iBGP** - Full Mesh 不可扩展，RR 复杂易错
3. **AS 边界对齐故障域** - 最小化故障影响范围
4. **自动化配置管理** - 减少人工错误

**最终建议**:
- ✅ **生产环境强烈推荐方案 1**（一个节点一个 AS）
- ✅ **特殊场景可考虑方案 2**（一个 TOR 一个 AS）
- ⚠️ **谨慎评估方案 3**（一个 Leaf 一个 AS）
- ❌ **不推荐方案 4/5**（大范围 iBGP）
