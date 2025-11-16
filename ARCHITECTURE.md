# 网络拓扑感知的 IPAM 架构设计

## 1. 背景和需求

### 真实数据中心场景

在实际的数据中心环境中，网络拓扑具有以下特点：

1. **分层架构**：Zone（可用区）-> Pod（机柜组）-> TOR（Top of Rack交换机）-> Node（节点）
2. **TOR 约束**：同一 TOR 下的节点通常需要使用相同或相邻的网段，以优化路由
3. **网段共享**：一个网段可能跨多个节点，一个节点也可能使用多个网段
4. **灵活分配**：支持按需分配，而不是预分配固定大小的块

### 设计目标

- ✅ 支持网络拓扑感知的网段分配
- ✅ TOR 级别的网段池管理
- ✅ 节点可以从多个网段分配 IP
- ✅ 网段可以跨节点共享
- ✅ 支持不同的分配策略（TOR级、Pod级、Zone级）
- ✅ 保持高性能和高可用

## 2. 架构设计

### 2.1 网络拓扑模型

```
┌─────────────────────────────────────────────────────────────┐
│                         IPAM Cluster                         │
│                                                              │
│  ┌────────────────────────────────────────────────────┐     │
│  │              Global Subnet Pool                    │     │
│  │                                                     │     │
│  │   10.244.0.0/16 (Cluster CIDR)                    │     │
│  │   ├─ 10.244.0.0/20  -> Zone A                     │     │
│  │   ├─ 10.244.16.0/20 -> Zone B                     │     │
│  │   └─ 10.244.32.0/20 -> Zone C                     │     │
│  └────────────────────────────────────────────────────┘     │
│                          │                                   │
│                          ▼                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                   Zone A                             │    │
│  │                                                      │    │
│  │  ┌──────────────────┐      ┌──────────────────┐    │    │
│  │  │    Pod 1         │      │    Pod 2         │    │    │
│  │  │                  │      │                  │    │    │
│  │  │  ┌────────────┐  │      │  ┌────────────┐ │    │    │
│  │  │  │  TOR 1     │  │      │  │  TOR 3     │ │    │    │
│  │  │  │  10.244.0  │  │      │  │  10.244.8  │ │    │    │
│  │  │  │            │  │      │  │            │ │    │    │
│  │  │  │ ┌────┐     │  │      │  │ ┌────┐     │ │    │    │
│  │  │  │ │N1  │     │  │      │  │ │N5  │     │ │    │    │
│  │  │  │ │N2  │     │  │      │  │ │N6  │     │ │    │    │
│  │  │  │ └────┘     │  │      │  │ └────┘     │ │    │    │
│  │  │  └────────────┘  │      │  └────────────┘ │    │    │
│  │  │                  │      │                  │    │    │
│  │  │  ┌────────────┐  │      │  ┌────────────┐ │    │    │
│  │  │  │  TOR 2     │  │      │  │  TOR 4     │ │    │    │
│  │  │  │  10.244.4  │  │      │  │  10.244.12 │ │    │    │
│  │  │  │            │  │      │  │            │ │    │    │
│  │  │  │ ┌────┐     │  │      │  │ ┌────┐     │ │    │    │
│  │  │  │ │N3  │     │  │      │  │ │N7  │     │ │    │    │
│  │  │  │ │N4  │     │  │      │  │ │N8  │     │ │    │    │
│  │  │  │ └────┘     │  │      │  │ └────┘     │ │    │    │
│  │  │  └────────────┘  │      │  └────────────┘ │    │    │
│  │  └──────────────────┘      └──────────────────┘    │    │
│  └─────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

### 2.2 核心概念

#### Zone (可用区)
- 逻辑隔离的网络区域
- 通常对应物理数据中心或可用区
- 拥有独立的网段池

#### Pod (机柜组)
- 一组物理机柜的集合
- 共享网络出口
- 内部 TOR 之间高速互联

#### TOR (Top of Rack 交换机)
- 机柜顶部交换机
- 连接一组物理节点（通常 20-40 台）
- **关键约束**：同一 TOR 下的节点应使用相同或相邻的网段

#### Node (节点/宿主机)
- 物理服务器或虚拟机
- 可以从多个网段分配 IP
- 属于特定的 TOR

### 2.3 网段分配策略

#### 策略 1: TOR 级别网段池（默认）

每个 TOR 拥有一个或多个网段，同一 TOR 下的所有节点共享这些网段。

```
TOR-1: [10.244.0.0/22]  (1024 IPs)
  ├─ Node-1: 从 10.244.0.0/22 分配
  ├─ Node-2: 从 10.244.0.0/22 分配
  └─ Node-3: 从 10.244.0.0/22 分配

TOR-2: [10.244.4.0/22]  (1024 IPs)
  ├─ Node-4: 从 10.244.4.0/22 分配
  └─ Node-5: 从 10.244.4.0/22 分配
```

**优点**：
- 符合物理网络拓扑
- 路由表小，路由效率高
- 故障隔离（TOR 级别）

#### 策略 2: 节点多网段

节点可以从多个网段分配 IP，支持更灵活的部署。

```
Node-1:
  ├─ Subnet-A: 10.244.0.0/24   (默认网段)
  ├─ Subnet-B: 10.244.100.0/24 (特殊用途网段)
  └─ Subnet-C: 2001:db8::/120  (IPv6 网段)
```

**用途**：
- 多租户隔离
- 不同网络平面（管理、数据、存储）
- IPv4 + IPv6 双栈

#### 策略 3: 网段共享

一个网段可以跨多个节点，提高 IP 利用率。

```
Subnet: 10.244.0.0/22
  ├─ Node-1: 分配了 50 个 IP
  ├─ Node-2: 分配了 30 个 IP
  ├─ Node-3: 分配了 100 个 IP
  └─ Available: 844 IPs
```

## 3. 数据模型

### 3.1 拓扑定义

```go
// Zone 代表一个可用区
type Zone struct {
    ID          string
    Name        string
    SubnetRanges []string  // e.g., ["10.244.0.0/20"]
    Pods        []*Pod
}

// Pod 代表一个机柜组
type Pod struct {
    ID          string
    Name        string
    ZoneID      string
    SubnetRanges []string  // e.g., ["10.244.0.0/21"]
    TORs        []*TOR
}

// TOR 代表一个 Top of Rack 交换机
type TOR struct {
    ID          string
    Name        string
    PodID       string
    Location    string     // 物理位置
    SubnetPool  *SubnetPool
    Nodes       []string   // Node IDs
}

// Node 代表一个节点
type Node struct {
    ID          string
    Name        string
    TORID       string
    Labels      map[string]string
    Subnets     []*NodeSubnet
}

// NodeSubnet 代表节点使用的网段
type NodeSubnet struct {
    SubnetCIDR  string
    Purpose     string  // "default", "storage", "management"
    AllocatedIPs int
    Capacity    int
}
```

### 3.2 网段池

```go
// SubnetPool 管理一组网段
type SubnetPool struct {
    ID          string
    TORID       string      // 所属 TOR
    Subnets     []*Subnet
    mu          sync.RWMutex
}

// Subnet 代表一个 CIDR 网段
type Subnet struct {
    CIDR        *net.IPNet
    Purpose     string
    Capacity    int
    Used        int
    Allocations map[string]*IPAllocation  // IP -> Allocation
    bitmap      *allocator.Bitmap
}

// IPAllocation 记录单个 IP 的分配信息
type IPAllocation struct {
    IP           string
    NodeID       string
    ContainerID  string
    PodName      string
    Namespace    string
    AllocatedAt  time.Time
}
```

## 4. 分配流程

### 4.1 初始化流程

```
1. 定义网络拓扑
   ├─ 创建 Zone（可用区）
   ├─ 创建 Pod（机柜组）
   └─ 创建 TOR（交换机）

2. 分配网段
   ├─ Zone 获得大网段 (e.g., /16)
   ├─ Pod 获得中网段 (e.g., /20)
   └─ TOR 获得小网段 (e.g., /22)

3. 节点注册
   ├─ 节点注册时声明所属 TOR
   └─ 自动关联 TOR 的网段池
```

### 4.2 IP 分配流程

```
CNI Request: 为 Pod 分配 IP
  │
  ├─ 1. 查询节点所属 TOR
  │     SELECT tor FROM nodes WHERE node_id = ?
  │
  ├─ 2. 获取 TOR 的网段池
  │     SELECT subnets FROM tor_subnet_pools WHERE tor_id = ?
  │
  ├─ 3. 选择网段（策略）
  │     ├─ 优先使用使用率低的网段
  │     ├─ 考虑网段用途（default, storage, etc.）
  │     └─ 检查可用容量
  │
  ├─ 4. 从网段分配 IP
  │     ├─ bitmap.FindFirstZero()
  │     └─ 标记为已使用
  │
  ├─ 5. 记录分配信息
  │     ├─ 保存到 Raft（持久化）
  │     └─ 更新本地缓存
  │
  └─ 6. 返回 IP 信息
        └─ {IP, Gateway, Routes, Subnet}
```

### 4.3 网段扩展流程

```
检测到网段即将耗尽 (使用率 > 80%)
  │
  ├─ 1. Raft Leader 收到扩展请求
  │
  ├─ 2. 从全局池分配新网段
  │     ├─ 选择相邻网段（路由聚合）
  │     └─ 大小根据历史使用率计算
  │
  ├─ 3. 添加到 TOR 网段池
  │     ├─ 通过 Raft 提交
  │     └─ 复制到所有节点
  │
  └─ 4. 通知相关节点
        └─ 更新本地网段列表
```

## 5. Raft 状态机

### 5.1 操作类型

```go
type CommandType string

const (
    // 拓扑管理
    CommandRegisterTOR    CommandType = "register_tor"
    CommandRegisterNode   CommandType = "register_node"

    // 网段管理
    CommandAllocateSubnet CommandType = "allocate_subnet"
    CommandReleaseSubnet  CommandType = "release_subnet"
    CommandExpandSubnet   CommandType = "expand_subnet"

    // IP 分配
    CommandAllocateIP     CommandType = "allocate_ip"
    CommandReleaseIP      CommandType = "release_ip"
    CommandBatchUpdate    CommandType = "batch_update"
)
```

### 5.2 状态机数据

```go
type TopologyState struct {
    Zones    map[string]*Zone
    Pods     map[string]*Pod
    TORs     map[string]*TOR
    Nodes    map[string]*Node

    GlobalSubnetPool *GlobalSubnetPool
    TORSubnetPools   map[string]*SubnetPool  // tor_id -> SubnetPool

    mu sync.RWMutex
}
```

## 6. 性能优化

### 6.1 本地缓存

每个节点缓存：
- 自己所属的 TOR 信息
- TOR 的网段池
- 本地分配的 IP 列表

### 6.2 批量操作

- 批量 IP 分配（一次分配多个）
- 批量更新使用统计
- 批量同步状态

### 6.3 分层路由

```
Level 1: 本地 Bitmap 查找 (< 1μs)
Level 2: TOR 网段池查找 (< 10μs)
Level 3: Raft 全局分配 (< 100ms)
```

## 7. 配置示例

### 7.1 拓扑配置

```yaml
topology:
  zones:
    - id: zone-a
      name: "Beijing Zone A"
      cidr: "10.244.0.0/16"

      pods:
        - id: pod-1
          name: "Pod 1 - Rack 1-10"
          cidr: "10.244.0.0/20"

          tors:
            - id: tor-1
              name: "TOR-R01-01"
              location: "Rack 01"
              subnets:
                - cidr: "10.244.0.0/22"
                  purpose: "default"
                - cidr: "10.244.4.0/24"
                  purpose: "storage"

            - id: tor-2
              name: "TOR-R02-01"
              location: "Rack 02"
              subnets:
                - cidr: "10.244.8.0/22"
                  purpose: "default"
```

### 7.2 节点注册

```yaml
node:
  id: "node-01"
  name: "k8s-node-01"
  tor_id: "tor-1"
  labels:
    rack: "R01"
    zone: "zone-a"

  subnet_preferences:
    - purpose: "default"
      min_available: 100
    - purpose: "storage"
      min_available: 50
```

## 8. 监控指标

新增 Prometheus 指标：

```
# TOR 级别
ipam_tor_subnets_total{tor_id, purpose}
ipam_tor_ip_capacity{tor_id}
ipam_tor_ip_used{tor_id}
ipam_tor_ip_available{tor_id}

# 网段级别
ipam_subnet_usage_ratio{subnet_cidr, tor_id}
ipam_subnet_allocations_total{subnet_cidr}

# 拓扑级别
ipam_zones_total
ipam_pods_total{zone_id}
ipam_tors_total{pod_id}
ipam_nodes_total{tor_id}
```

## 9. 迁移路径

### 从 v0.2.0 迁移到新架构

1. **兼容模式**：保持旧 API，内部映射到新架构
2. **默认拓扑**：自动为每个节点创建虚拟 TOR
3. **逐步迁移**：允许部分节点使用新架构
4. **配置转换工具**：自动将旧配置转换为新拓扑

## 10. 优势总结

✅ **拓扑感知**：符合真实数据中心网络架构
✅ **灵活分配**：支持多种分配策略
✅ **高效路由**：TOR 级别聚合，减小路由表
✅ **弹性扩展**：动态添加网段
✅ **多网段**：节点可使用多个网段
✅ **高性能**：保持 < 1μs 分配延迟
✅ **高可用**：Raft 保证一致性

---

**下一步实现优先级：**
1. 实现 Topology 数据模型
2. 实现 SubnetPool 管理
3. 重构 Pool 支持 TOR 级分配
4. 更新 Raft FSM
5. 添加迁移工具
