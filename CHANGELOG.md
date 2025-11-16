# Changelog

All notable changes to this project will be documented in this file.

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
