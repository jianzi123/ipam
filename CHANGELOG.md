# Changelog

All notable changes to this project will be documented in this file.

## [0.3.0] - 2025-11-16

### Added - 拓扑感知架构

#### 1. 网络拓扑管理 (pkg/topology/)
- **四层拓扑结构**: Zone → Pod → TOR → Node
- `Topology`: 核心拓扑管理器
  - `AddZone`: 添加可用区
  - `AddPod`: 添加 Pod（机房/机柜组）
  - `AddTOR`: 添加 TOR（Top of Rack 交换机）
  - `RegisterNode`: 注册节点到拓扑
  - `GetNodeTOR`: 获取节点所属 TOR
  - `GetNodePath`: 获取节点完整路径（Zone/Pod/TOR/Node）
- `SubnetPool`: TOR 级网段池
  - 支持多用途网段（default、storage、management）
  - `AddSubnet`: 添加网段到 TOR
  - `AllocateIP`: 基于用途分配 IP
  - `ReleaseIP`: 释放 IP
  - `GetStats`: 获取网段池统计信息
- 完整的单元测试覆盖

#### 2. 拓扑感知 IP 池 (pkg/ipam/topology_pool.go)
- `TopologyAwarePool`: 拓扑感知的 IP 池
- `InitializeTopology`: 从 JSON 配置初始化拓扑
- `RegisterNode`: 注册节点到指定 TOR
- `AllocateIPForNode`: 基于节点拓扑位置分配 IP
- `AllocateIPForNodeWithPurpose`: 分配特定用途的 IP
- `ReleaseIPForNode`: 释放节点 IP
- `AddSubnetToTOR`: 动态添加网段到 TOR
- `GetNodeStats`: 获取节点详细统计（含拓扑路径）
- JSON 配置支持（TopologyConfig）

#### 3. 拓扑感知 Raft FSM (pkg/raft/topology_fsm.go)
- `TopologyFSM`: 拓扑操作的 Raft 状态机
- 支持的命令:
  - `CommandInitTopology`: 初始化拓扑配置
  - `CommandRegisterNode`: 注册节点
  - `CommandAllocateIP`: 分配 IP（拓扑感知）
  - `CommandReleaseIP`: 释放 IP
  - `CommandAddSubnet`: 动态添加网段到 TOR
- 快照和恢复支持
- 完整的单元测试（7 个测试用例）

#### 4. 拓扑感知 Raft 节点 (pkg/raft/topology_node.go)
- `TopologyNode`: 拓扑感知的 Raft 节点
- `InitializeTopology`: 通过 Raft 共识初始化拓扑
- `RegisterNode`: 通过 Raft 共识注册节点
- `AllocateIP`: 通过 Raft 共识分配 IP
- `ReleaseIP`: 通过 Raft 共识释放 IP
- `AddSubnetToTOR`: 通过 Raft 共识添加网段

#### 5. 架构文档 (ARCHITECTURE.md)
- 完整的拓扑架构设计文档
- 三种分配策略详解
- 数据模型和流程图
- 性能优化策略
- 与传统方案的对比

### Features

- **灵活网段映射**
  - 一个节点可以从多个网段获取 IP（支持不同用途）
  - 一个网段可以被多个节点共享
  - 支持运行时动态添加网段

- **多用途网段支持**
  - `default`: 默认 Pod 网络
  - `storage`: 存储网络
  - `management`: 管理网络
  - 可扩展到自定义用途

- **真实数据中心匹配**
  - 拓扑结构匹配物理机房布局
  - TOR 级网段池符合实际网络架构
  - 支持跨机架网段共享

### Changed
- 更新 README.md 添加 v0.3.0 拓扑架构说明
- 更新项目结构文档（新增 topology 包）
- 修复 cmd/cni-plugin/main.go 未使用的 import

### Testing
- 所有测试通过 ✓
- pkg/topology: PASS (SubnetPool + Topology)
- pkg/ipam: PASS (TopologyAwarePool)
- pkg/raft: PASS (TopologyFSM)
- 测试覆盖率: 拓扑相关代码 > 90%

### Documentation
- ARCHITECTURE.md: 拓扑架构完整设计文档
- README.md: 新增拓扑架构介绍和示例
- CHANGELOG.md: 详细的版本变更记录

## [0.2.0] - 2025-11-16

### Added

#### 1. gRPC 服务实现 (pkg/server/)
- 完整的 IPAM gRPC 服务器实现
- `AllocateIP`: 为 Pod 分配 IP 地址
- `ReleaseIP`: 释放 IP 地址
- `GetNodeBlocks`: 查询节点的 IP 块
- `GetPoolStats`: 获取池统计信息
- 自动预分配机制（当可用 IP < 20% 时自动分配新块）
- 支持 Unix Socket 和 TCP 双模式

#### 2. 持久化存储 (pkg/store/)
- 基于 BoltDB 的持久化存储
- Container ID -> IP 映射存储
- `SaveIPMapping`: 保存 IP 映射
- `GetIPMapping`: 通过容器 ID 查询 IP
- `ListMappingsByNode`: 按节点列出映射
- `GetMappingByIP`: 通过 IP 反向查询
- `CleanupStaleEntries`: 清理过期条目
- 完整的单元测试覆盖

#### 3. Prometheus 监控 (pkg/metrics/)
- 40+ Prometheus 指标
- IP 分配/释放计数器
- 延迟分布直方图
- Per-node IP 使用率 gauge
- Block 使用率监控
- Raft 状态指标
- Store 操作统计
- 自动 metrics 收集器（10 秒间隔）

#### 4. IPv6 支持 (pkg/allocator/ipv6.go)
- `IPv6Block`: 完整的 IPv6 地址块管理
- 支持 /64 到 /120 的 IPv6 CIDR
- `DualStackBlock`: 双栈（IPv4 + IPv6）分配
- 使用 `big.Int` 处理大地址空间
- IPv6 位置 <-> IP 地址转换
- 原子性双栈分配和释放操作
- 完整的单元测试

#### 5. 增强的 IPAM Daemon
- 集成 gRPC 服务器（Unix Socket: `/run/ipam/ipam.sock`，TCP: `:9090`）
- 启用持久化存储（默认：`/var/lib/ipam/ipam.db`）
- Prometheus metrics 服务器（`:2112/metrics`）
- 自动 metrics 收集
- 优雅关闭流程
- 新增命令行参数：
  - `--metrics-addr`: Prometheus metrics 地址
  - `--enable-store`: 启用/禁用持久化存储

### Changed
- 更新 README.md 反映新功能
- 添加 v0.2.0 路线图
- 更新性能指标（< 0.3μs 分配延迟）
- 更新项目结构文档

### Performance
- Bitmap 分配: 8.7 ns/op（零内存分配）
- IP 块分配: 297 ns/op
- 实际吞吐量: > 400 万 allocations/s
- IPv6 分配性能与 IPv4 相当

### Testing
- 所有测试通过 ✓
- pkg/allocator: PASS（包括 IPv6）
- pkg/ipam: PASS
- pkg/store: PASS
- 测试覆盖率提升

## [0.1.0] - 2025-11-15

### Added
- 核心 IP 分配算法（Bitmap）
- IP 池管理
- Raft 共识集成（HashiCorp Raft）
- CNI 插件接口（ADD/DEL/CHECK/VERSION）
- IPAM 守护进程
- 基础单元测试
- 完整的设计文档（DESIGN.md）
- 项目文档（README.md）

### Performance
- IP 分配延迟: < 1μs
- 吞吐量: > 1 万 allocations/s per node

---

**版本格式**: [Major.Minor.Patch]
- Major: 不兼容的 API 更改
- Minor: 向后兼容的功能添加
- Patch: 向后兼容的 bug 修复
