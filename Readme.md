# Kubernetes CNI IPAM - 高性能 IP 地址管理系统

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

一个为 Kubernetes CNI 设计的高性能 IP 地址管理（IPAM）系统，采用 **两层分配架构 + Raft 共识** 实现，支持多副本、高可用和极低延迟的 IP 分配。

## 🌟 核心特性

- **🚀 极致性能**: 本地 bitmap 分配，< 0.3μs 延迟，> 400万 allocations/s per node
- **🏗️ 拓扑感知架构** (v0.3.0):
  - 四层网络拓扑：Zone → Pod → TOR → Node
  - TOR 级网段池管理，匹配真实数据中心架构
  - 灵活的网段分配：支持多网段/节点、网段共享
- **🌐 BGP 三层网络支持**:
  - Leaf-Spine-TOR 网络拓扑集成
  - 容器 IP 以 /32 路由通过 BGP 发布
  - eBGP 架构，支持大规模扩展
  - 纯三层路由，无 overlay 封装
- **🔄 两层分配架构**:
  - L1: Raft 管理拓扑和网段分配（低频操作）
  - L2: 节点本地管理 Pod IP 分配（高频操作）
- **💪 高可用**: 基于 HashiCorp Raft 的多副本一致性
- **📊 智能管理**: 预分配、批量操作、动态扩展
- **🔌 CNI 兼容**: 完全遵循 CNI 0.4.0/1.0.0 规范
- **📈 可观测性**: Prometheus 监控（40+ metrics）+ 结构化日志
- **💾 持久化存储**: BoltDB 存储容器 ID -> IP 映射
- **🌌 IPv6 就绪**: 完整的双栈（IPv4+IPv6）和 IPv6-only 支持

## 📋 目录

- [架构设计](#架构设计)
- [快速开始](#快速开始)
- [使用指南](#使用指南)
- [性能测试](#性能测试)
- [配置说明](#配置说明)
- [开发指南](#开发指南)
- [路线图](#路线图)

## 🏗️ 架构设计

### 整体架构

```
┌─────────────────────────────────────────────────────┐
│              Kubernetes Cluster                      │
│                                                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │  Node 1  │  │  Node 2  │  │  Node 3  │          │
│  │ CNI      │  │ CNI      │  │ CNI      │          │
│  │ Plugin   │  │ Plugin   │  │ Plugin   │          │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘          │
│       │             │             │                 │
│       └─────────────┼─────────────┘                 │
│                     │                               │
│              gRPC API (< 1ms)                       │
│                     │                               │
│  ┌──────────────────┼────────────────────────┐     │
│  │         IPAM Daemon Cluster               │     │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐   │     │
│  │  │ IPAM-1  │  │ IPAM-2  │  │ IPAM-3  │   │     │
│  │  │(Leader) │  │(Follower)│ │(Follower)│   │     │
│  │  └────┬────┘  └────┬────┘  └────┬─────┘   │     │
│  │       └────────────┼────────────┘          │     │
│  │            Raft Consensus                  │     │
│  │       ┌────────────┴────────────┐          │     │
│  │       │   Replicated State      │          │     │
│  │       │  - Node -> IP Block Map │          │     │
│  │       └─────────────────────────┘          │     │
│  └────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────┘
```

### 关键设计

**1. 两层分配策略**
- **节点级（通过 Raft）**: 为节点分配 /24 IP 块，保证全局无冲突
- **Pod 级（本地操作）**: 从本地 IP 块快速分配，无需 RPC

**2. 性能优化**
- Bitmap 快速查找: O(1) 平均时间复杂度
- 预分配机制: 剩余 20% 时自动申请新块
- 批量更新: 减少 Raft 写入频率

**3. 高可用性**
- Raft 3/5 节点集群
- Leader 故障自动切换 < 1s
- 持久化存储（BoltDB）

### 拓扑感知架构 (v0.3.0)

v0.3.0 引入了**网络拓扑感知**的 IP 分配架构，匹配真实数据中心的网络层次结构：

```
Zone (可用区)
  └─ Pod (机房/机柜组)
      └─ TOR (Top of Rack 交换机)
          ├─ Subnet Pool (网段池)
          │   ├─ 10.244.0.0/24  (default)
          │   ├─ 10.244.100.0/24 (storage)
          │   └─ 10.244.200.0/24 (management)
          └─ Nodes (物理节点)
              ├─ Node 1
              ├─ Node 2
              └─ Node 3
```

**核心优势**：

1. **灵活的网段分配**
   - 一个节点可以从多个网段获取 IP（不同用途）
   - 一个网段可以被多个节点共享
   - 支持动态添加网段到 TOR

2. **拓扑配置示例**
```json
{
  "zones": [{
    "id": "zone-a",
    "name": "Beijing Zone A",
    "pods": [{
      "id": "pod-1",
      "name": "Pod 1",
      "tors": [{
        "id": "tor-1",
        "name": "TOR-R01",
        "location": "Rack 01",
        "subnets": [
          {"cidr": "10.244.0.0/24", "purpose": "default"},
          {"cidr": "10.244.100.0/24", "purpose": "storage"}
        ]
      }]
    }]
  }]
}
```

3. **多用途网段**
   - `default`: 默认 Pod 网络
   - `storage`: 存储网络
   - `management`: 管理网络

### BGP 三层网络架构 (可选)

本项目支持与 BGP 网络深度集成，实现纯三层路由的容器网络：

```
Spine Layer (AS 65000)
  ├─ Spine-1 ←→ Spine-2 (iBGP)
  ↓
Leaf Layer (AS 65001-6500N)
  ├─ Leaf-1 (AS 65001) ←→ Spine (eBGP)
  ├─ Leaf-2 (AS 65002) ←→ Spine (eBGP)
  ↓
TOR Layer (L2 Switch)
  ├─ TOR-1 → Leaf-1/Leaf-2
  ↓
Node Layer (AS 4200000001-420000000N)
  ├─ Node-1 (AS 4200000001) ←→ Leaf-1 (eBGP)
  ├─ Node-2 (AS 4200000002) ←→ Leaf-1 (eBGP)
  ↓
Container /32 路由发布
  ├─ 10.244.1.5/32 via Node-1
  ├─ 10.244.1.6/32 via Node-1
```

**核心优势**:
- **无 overlay 开销**: 纯三层路由，无 VXLAN/IPIP 封装
- **ECMP 负载均衡**: 多路径自动负载均衡
- **快速收敛**: BGP + BFD 实现亚秒级故障检测
- **大规模扩展**: eBGP 架构支持数万节点

**配置示例**: [configs/](configs/)
- `bird-node-example.conf` - BIRD BGP 配置
- `calico-bgp-config.yaml` - Calico BGP 集成
- `topology-bgp-example.json` - 拓扑 + BGP 配置

详细设计文档:
- [DESIGN.md](DESIGN.md) - 核心 IPAM 设计
- [ARCHITECTURE.md](ARCHITECTURE.md) - 拓扑架构设计
- **[BGP_NETWORK_DESIGN.md](BGP_NETWORK_DESIGN.md) - BGP 三层网络设计** 🆕

## 🚀 快速开始

### 前置要求

- Go 1.21+
- Kubernetes 1.20+
- 3+ 节点用于 Raft 集群（推荐）

### 编译

```bash
# 克隆仓库
git clone https://github.com/jianzi123/ipam.git
cd ipam

# 安装依赖
make install-deps

# 编译所有组件
make build

# 运行测试
make test
```

编译产物：
- `bin/ipam-daemon` - IPAM 守护进程
- `bin/cni-plugin` - CNI 插件
- `bin/ipam-cli` - 管理工具

### 部署

**1. 启动 IPAM 集群（3 节点示例）**

节点 1（Bootstrap）:
```bash
./bin/ipam-daemon \
  --node-id=ipam-1 \
  --bind-addr=0.0.0.0:7000 \
  --cluster-cidr=10.244.0.0/16 \
  --bootstrap
```

节点 2:
```bash
./bin/ipam-daemon \
  --node-id=ipam-2 \
  --bind-addr=0.0.0.0:7000 \
  --join=ipam-1:7000
```

节点 3:
```bash
./bin/ipam-daemon \
  --node-id=ipam-3 \
  --bind-addr=0.0.0.0:7000 \
  --join=ipam-1:7000
```

**2. 配置 CNI**

复制 CNI 配置到 K8s 节点:
```bash
# 复制 CNI 插件
cp bin/cni-plugin /opt/cni/bin/

# 配置 CNI
cat > /etc/cni/net.d/10-ipam.conf <<EOF
{
  "cniVersion": "0.4.0",
  "name": "k8s-pod-network",
  "type": "ipam-cni",
  "ipam": {
    "type": "ipam-plugin",
    "daemonSocket": "/run/ipam/ipam.sock",
    "clusterCIDR": "10.244.0.0/16",
    "nodeBlockSize": 24,
    "routes": [{"dst": "0.0.0.0/0"}]
  }
}
EOF
```

**3. 验证**

```bash
# 查看集群状态
./bin/ipam-cli stats

# 查看节点 IP 块
./bin/ipam-cli blocks node1

# 分配测试块
./bin/ipam-cli allocate node1
```

## 📖 使用指南

### CNI 配置

```json
{
  "cniVersion": "0.4.0",
  "name": "k8s-pod-network",
  "type": "ipam-cni",
  "ipam": {
    "type": "ipam-plugin",
    "daemonSocket": "/run/ipam/ipam.sock",
    "clusterCIDR": "10.244.0.0/16",
    "nodeBlockSize": 24
  }
}
```

### IPAM Daemon 配置

```yaml
cluster:
  cidr: "10.244.0.0/16"
  nodeBlockSize: 24  # /24 = 254 IPs per node

raft:
  nodeID: "ipam-1"
  bindAddr: "0.0.0.0:7000"
  dataDir: "/var/lib/ipam/raft"
  bootstrap: true

grpc:
  bindAddr: "0.0.0.0:9090"
  unixSocket: "/run/ipam/ipam.sock"
```

### 管理命令

```bash
# 查看统计信息
ipam-cli stats

# 查看节点的 IP 块
ipam-cli blocks <node-id>

# 手动分配 IP 块
ipam-cli allocate <node-id>

# 释放 IP 块
ipam-cli release <node-id> <cidr>
```

## 📊 性能测试

### 测试结果

在标准测试环境下（3 节点 Raft 集群）:

| 操作 | 延迟 | 吞吐量 |
|------|------|--------|
| IP 分配（本地） | < 1ms | > 10,000 ops/s |
| IP 释放（本地） | < 1ms | > 10,000 ops/s |
| 块分配（Raft） | < 100ms | > 100 ops/s |

### 运行 Benchmark

```bash
# Bitmap 分配性能
go test -bench=BenchmarkBitmapSet ./pkg/allocator

# IP 块分配性能
go test -bench=BenchmarkIPBlock ./pkg/allocator

# Pool 分配性能
go test -bench=BenchmarkPool ./pkg/ipam
```

示例输出:
```
BenchmarkBitmapSet-8              50000000    25.3 ns/op
BenchmarkIPBlockAllocate-8        10000000    120 ns/op
BenchmarkPoolAllocateIP-8          5000000    300 ns/op
```

## ⚙️ 配置说明

### 网段规划

```
Cluster CIDR: 10.244.0.0/16 (65536 IPs)
  ├─ Node 1: 10.244.1.0/24  (254 IPs)
  ├─ Node 2: 10.244.2.0/24  (254 IPs)
  ├─ Node 3: 10.244.3.0/24  (254 IPs)
  └─ ...
```

- 默认每节点 /24 (254 个可用 IP)
- 可配置为 /25 (126 IPs) 或 /23 (510 IPs)
- 支持节点自动扩展

### Raft 调优

```yaml
raft:
  heartbeatTimeout: 1s    # 心跳超时
  electionTimeout: 1s     # 选举超时
  commitTimeout: 1s       # 提交超时
  snapshotInterval: 10000 # 快照间隔（日志数）
```

## 🛠️ 开发指南

### 项目结构

```
ipam/
├── pkg/
│   ├── allocator/      # IP 分配器（bitmap + IPv6）
│   ├── ipam/          # IP 池管理（含拓扑感知池）
│   ├── topology/      # 网络拓扑管理（Zone/Pod/TOR/Node）
│   ├── raft/          # Raft 集成（含拓扑 FSM）
│   ├── store/         # 持久化存储（BoltDB）
│   ├── server/        # gRPC 服务器
│   ├── metrics/       # Prometheus 指标
│   ├── cni/           # CNI 类型定义
│   └── api/proto/     # gRPC API 定义
├── cmd/
│   ├── ipam-daemon/   # IPAM 守护进程（含 gRPC + metrics）
│   ├── cni-plugin/    # CNI 插件
│   └── ipam-cli/      # 管理工具
├── configs/           # 配置示例（含拓扑配置）
├── docs/              # 设计文档
│   ├── DESIGN.md      # 原始设计文档
│   └── ARCHITECTURE.md # 拓扑架构文档
└── test/             # 集成测试

```

### 测试

```bash
# 单元测试
make test

# 性能测试
make bench

# 代码覆盖率
go test -cover ./...
```

### 贡献

欢迎贡献！请遵循以下步骤：

1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/amazing`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing`)
5. 创建 Pull Request

## 🗺️ 路线图

### ✅ v0.1.0 (已完成)

- [x] 核心 IP 分配算法（Bitmap）
- [x] IP 池管理
- [x] Raft 共识集成
- [x] CNI 插件接口
- [x] IPAM 守护进程
- [x] 基础测试

### ✅ v0.2.0 (已完成)

- [x] gRPC 服务实现
- [x] Prometheus 监控指标（40+ metrics）
- [x] 容器 ID -> IP 映射持久化（BoltDB）
- [x] IPv6 完整支持（双栈 + IPv6-only）
- [x] 性能优化（预分配、批量操作）
- [x] 完善的错误处理和日志

### ✅ v0.3.0 (已完成)

- [x] **拓扑感知架构**：Zone/Pod/TOR/Node 四层网络拓扑
- [x] **TOR 级网段池**：支持机架级网段管理
- [x] **灵活网段映射**：一个节点可有多个网段，一个网段可跨多个节点
- [x] **多用途网段**：default、storage、management 等不同用途
- [x] **拓扑感知 Raft FSM**：支持拓扑配置的分布式一致性
- [x] **动态网段扩展**：支持运行时为 TOR 添加新网段

### 🚧 v0.4.0 (进行中)

- [ ] 完整的 CNI 网络配置（veth pair setup）
- [ ] gRPC mTLS 认证
- [ ] IP 地址回收策略优化
- [ ] 健康检查 API

### 📅 v0.4.0+ (计划中)

- [ ] 多网络平面支持
- [ ] IP 地址池动态扩展
- [ ] 云平台集成（AWS VPC、Azure VNET）
- [ ] WebUI 管理界面
- [ ] Helm Chart 部署
- [ ] 性能基准测试套件

## 📊 与其他方案对比

| 特性 | 本方案 | Calico | Cilium | Whereabouts |
|------|--------|--------|---------|-------------|
| 分配模式 | 两层分配 | IPAM Pool | Cluster Pool | Cluster-wide |
| 一致性 | Raft | etcd | etcd/KVStore | K8s API |
| 分配延迟 | < 1ms | ~10ms | ~5ms | ~50ms |
| 高可用 | 内置 Raft | 依赖 etcd | 依赖外部 | 依赖 K8s |
| 独立部署 | ✅ | ✅ | ✅ | ❌ |

## 📝 许可证

MIT License - 详见 [LICENSE](LICENSE)

## 🙏 致谢

- [HashiCorp Raft](https://github.com/hashicorp/raft) - Raft 共识库
- [CNI Specification](https://github.com/containernetworking/cni) - CNI 规范
- Calico、Cilium - 架构设计参考

## 📞 联系方式

- Issues: [GitHub Issues](https://github.com/jianzi123/ipam/issues)
- Email: jianzi123@example.com

---

**⚡ 高性能 | 🔄 高可用 | 🎯 易于使用**
