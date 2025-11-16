# K8s CNI IPAM 设计方案

## 1. 整体架构

基于业界最佳实践（Calico、Cilium、Whereabouts），我们采用 **两层分配 + Raft 共识** 的架构：

```
┌─────────────────────────────────────────────────────┐
│              Kubernetes Cluster                      │
│                                                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │  Node 1  │  │  Node 2  │  │  Node 3  │          │
│  │          │  │          │  │          │          │
│  │ CNI      │  │ CNI      │  │ CNI      │          │
│  │ Plugin   │  │ Plugin   │  │ Plugin   │          │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘          │
│       │             │             │                 │
│       └─────────────┼─────────────┘                 │
│                     │                               │
│              gRPC API (Fast Path)                   │
│                     │                               │
│  ┌──────────────────┼────────────────────────┐     │
│  │         IPAM Daemon Cluster               │     │
│  │                                            │     │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐   │     │
│  │  │ IPAM-1  │  │ IPAM-2  │  │ IPAM-3  │   │     │
│  │  │(Leader) │  │(Follower)│ │(Follower)│   │     │
│  │  └────┬────┘  └────┬────┘  └────┬─────┘   │     │
│  │       │            │            │          │     │
│  │       └────────────┼────────────┘          │     │
│  │                    │                       │     │
│  │            Raft Consensus                  │     │
│  │          (Node Block Allocation)           │     │
│  │                    │                       │     │
│  │       ┌────────────┴────────────┐          │     │
│  │       │   Replicated State      │          │     │
│  │       │  - Node -> IP Block Map │          │     │
│  │       │  - Block Allocation     │          │     │
│  │       └─────────────────────────┘          │     │
│  └────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────┘
```

## 2. 核心设计理念

### 2.1 两层分配架构（性能关键）

**L1 - 节点级块分配（通过 Raft）**
- IPAM 集群通过 Raft 为每个节点分配一个 IP 块（如 /24，254 个 IP）
- 低频操作，只在节点加入或 IP 块耗尽时发生
- 保证集群级别的 IP 不冲突

**L2 - Pod 级 IP 分配（节点本地）**
- CNI 插件在节点本地从已分配的 IP 块中分配 IP
- 高频操作，使用 bitmap 快速查找，O(1) 时间复杂度
- 无需 RPC 调用，极低延迟（< 1ms）

### 2.2 性能优化策略

1. **预分配（Pre-allocation）**
   - 节点启动时预分配 IP 块
   - 当剩余 IP < 20% 时，自动申请新块

2. **Bitmap 快速分配**
   - 使用 bitmap 数据结构
   - 查找首个空闲 IP：O(1) 平均复杂度
   - 支持 SIMD 加速（可选）

3. **本地缓存**
   - CNI 插件缓存节点的 IP 块信息
   - 避免每次分配都调用 IPAM API

4. **批量操作**
   - 批量更新 IP 使用状态
   - 减少 Raft 写入频率

## 3. IP 分配策略

### 3.1 网段规划

```
Cluster CIDR: 10.244.0.0/16 (65536 IPs)
  ├─ Node 1: 10.244.1.0/24  (254 IPs)
  ├─ Node 2: 10.244.2.0/24  (254 IPs)
  ├─ Node 3: 10.244.3.0/24  (254 IPs)
  └─ ...
```

- 默认每节点 /24 (254 个可用 IP)
- 可配置为 /25 (126 IPs) 或 /23 (510 IPs)
- 支持多个 IP 块分配给同一节点

### 3.2 IP 回收策略

- **即时标记**: Pod 删除时立即标记 IP 为可用
- **延迟回收**: 30 秒后才能重新分配（防止 TIME_WAIT 冲突）
- **定期清理**: 每 5 分钟清理孤儿 IP

## 4. Raft 共识实现

### 4.1 技术选型

使用 **HashiCorp Raft**（生产级别，被 Consul、Nomad 使用）

优势：
- 成熟稳定，经过大规模生产验证
- 完善的文档和社区支持
- 支持快照和日志压缩
- 性能优异（1000+ ops/s）

### 4.2 状态机设计

```go
type IPAMStateMachine struct {
    // Node -> [IP Blocks] mapping
    nodeBlocks map[string][]IPBlock

    // IP Block allocation status
    blockStatus map[string]BlockStatus

    // Global CIDR pool
    clusterCIDR *net.IPNet
}

type IPBlock struct {
    CIDR      string    // e.g., "10.244.1.0/24"
    NodeID    string    // Node identifier
    Used      int       // Used IP count
    Total     int       // Total IP count
    CreatedAt time.Time
}
```

### 4.3 操作类型

通过 Raft 的操作（需要共识）：
- `AllocateBlock`: 为节点分配 IP 块
- `ReleaseBlock`: 释放节点的 IP 块
- `UpdateBlockUsage`: 更新块使用统计（批量）

## 5. CNI 插件实现

### 5.1 CNI 规范

遵循 CNI 0.4.0 规范，实现四个命令：
- `ADD`: 为容器分配 IP
- `DEL`: 释放容器 IP
- `CHECK`: 检查容器网络配置
- `VERSION`: 返回插件版本

### 5.2 工作流程

**ADD 命令流程**:
```
1. CNI 插件接收 ADD 请求
2. 从本地缓存获取节点的 IP 块
3. 使用 bitmap 查找可用 IP（本地操作）
4. 配置容器网络接口
5. 返回分配的 IP 信息
6. 异步上报 IP 使用状态（批量）
```

**DEL 命令流程**:
```
1. CNI 插件接收 DEL 请求
2. 删除容器网络接口
3. 标记 IP 为可回收
4. 异步上报释放状态
```

## 6. 组件架构

### 6.1 核心组件

1. **ipam-daemon**
   - Raft 节点
   - gRPC 服务器
   - IP 块管理器
   - 健康检查

2. **cni-plugin**
   - 标准 CNI 可执行文件
   - 本地 IP 分配器（bitmap）
   - gRPC 客户端

3. **ipam-cli**
   - 管理工具
   - 查询 IP 分配状态
   - 手动操作（测试用）

### 6.2 通信方式

- **CNI Plugin <-> IPAM Daemon**: gRPC（高性能）
- **IPAM Daemon <-> IPAM Daemon**: Raft 协议
- **管理工具 <-> IPAM Daemon**: gRPC/REST API

## 7. 数据持久化

### 7.1 Raft 存储

- **日志存储**: BoltDB（内置于 HashiCorp Raft）
- **快照**: 每 10000 条日志触发
- **日志压缩**: 自动清理旧日志

### 7.2 本地缓存

- **IP 块缓存**: 内存中保存节点的 IP 块
- **Bitmap 持久化**: 保存到本地文件（/var/lib/cni/ipam/）
- **崩溃恢复**: 重启后从 IPAM daemon 重新同步

## 8. 高可用设计

### 8.1 IPAM Daemon 集群

- 至少 3 个节点（推荐 5 个）
- Leader 处理所有写请求
- Follower 提供读请求（stale read）
- Leader 故障时自动选举（< 1s）

### 8.2 CNI 插件容错

- 本地 IP 块缓存，IPAM daemon 不可用时仍可分配
- 重试机制：3 次重试 + 指数退避
- 降级策略：使用本地 IP 池

## 9. 性能指标

预期性能（基于设计）：

- **IP 分配延迟**: < 1ms（本地 bitmap 查找）
- **吞吐量**: > 10000 allocations/s per node
- **Raft 写入**: < 100ms（仅节点级块分配）
- **内存占用**:
  - CNI Plugin: < 10MB
  - IPAM Daemon: < 100MB (1000 nodes)

## 10. 监控和可观测性

### 10.1 Metrics (Prometheus)

- `ipam_block_allocations_total`: 块分配次数
- `ipam_ip_allocations_total`: IP 分配次数
- `ipam_allocation_duration_seconds`: 分配延迟
- `ipam_raft_leader`: Raft leader 状态
- `ipam_available_ips`: 可用 IP 数量

### 10.2 日志

- 结构化日志（JSON）
- 可配置日志级别
- 关键操作审计日志

## 11. 配置示例

### 11.1 CNI 配置

```json
{
  "cniVersion": "0.4.0",
  "name": "k8s-pod-network",
  "type": "ipam-cni",
  "ipam": {
    "type": "ipam-plugin",
    "daemonSocket": "/run/ipam/ipam.sock",
    "clusterCIDR": "10.244.0.0/16",
    "nodeBlockSize": 24,
    "routes": [
      { "dst": "0.0.0.0/0" }
    ]
  }
}
```

### 11.2 IPAM Daemon 配置

```yaml
cluster:
  cidr: "10.244.0.0/16"
  nodeBlockSize: 24  # /24 per node

raft:
  nodeID: "ipam-1"
  bindAddr: "0.0.0.0:7000"
  dataDir: "/var/lib/ipam/raft"
  peers:
    - "ipam-1:7000"
    - "ipam-2:7000"
    - "ipam-3:7000"

grpc:
  bindAddr: "0.0.0.0:9090"
  unixSocket: "/run/ipam/ipam.sock"

performance:
  batchInterval: 1s     # 批量更新间隔
  cacheSize: 1000       # 缓存大小
  preallocateThreshold: 0.2  # 20% 剩余时预分配
```

## 12. 实现路线图

### Phase 1: 核心功能（Week 1-2）
- [x] 项目结构搭建
- [ ] IP 地址池和 bitmap 分配器
- [ ] Raft 集成和状态机
- [ ] gRPC API 定义和实现

### Phase 2: CNI 集成（Week 3）
- [ ] CNI 插件实现
- [ ] 本地缓存和同步
- [ ] 配置文件解析

### Phase 3: 测试和优化（Week 4）
- [ ] 单元测试
- [ ] 集成测试
- [ ] 性能测试和优化
- [ ] 文档完善

## 13. 与业界方案对比

| 特性 | 本方案 | Calico | Cilium | Whereabouts |
|------|--------|--------|---------|-------------|
| 分配模式 | 两层分配 | IPAM Pool | Cluster Pool | Cluster-wide |
| 一致性 | Raft | etcd | etcd/KVStore | Kubernetes API |
| 分配延迟 | < 1ms | ~10ms | ~5ms | ~50ms |
| 高可用 | 内置 Raft | 依赖 etcd | 依赖外部 | 依赖 K8s API |
| 节点故障恢复 | 自动 | 自动 | 自动 | 手动 |

## 14. 安全考虑

- **认证**: mTLS for gRPC
- **授权**: 基于节点身份的访问控制
- **加密**: Raft 通信加密（可选）
- **审计**: 所有 IP 分配操作记录日志

## 15. 未来扩展

- IPv6 支持
- 多网络平面
- IP 地址池动态扩展
- 与云平台 IPAM 集成（AWS VPC、Azure VNET）
- WebUI 管理界面
