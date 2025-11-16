# IPAM 项目文档导航

本目录包含 Kubernetes CNI IPAM 项目的完整技术文档。

## 📖 快速导航

### 新手入门
1. **[项目 README](../Readme.md)** - 项目概览、快速开始、核心特性
2. **[版本历史](../CHANGELOG.md)** - 各版本更新记录

### 架构设计文档

#### IPAM 核心架构
- **[IPAM 核心设计](../DESIGN.md)** - 原始 IPAM 设计（两层分配架构、Raft 共识）
- **[拓扑感知架构](../ARCHITECTURE.md)** - v0.3.0 网络拓扑感知设计（Zone/Pod/TOR/Node）

> **阅读建议**: 先读 DESIGN.md 了解核心 IPAM 设计，再读 ARCHITECTURE.md 了解拓扑感知增强

### BGP 网络架构文档

BGP（Border Gateway Protocol）三层网络集成文档，按推荐阅读顺序排列：

1. **[BGP 网络设计](../BGP_NETWORK_DESIGN.md)** ⭐ **首选阅读**
   - Leaf-Spine-TOR 三层网络架构
   - 容器 IP 路由发布机制（/32 路由）
   - 数据包转发路径详解
   - BIRD/FRR 配置示例
   - **600+ 行完整指南**

2. **[iBGP vs eBGP 深度分析](../BGP_IBGP_EBGP_ANALYSIS.md)** 🔍 **深入理解**
   - BGP 协议基础
   - iBGP 与 eBGP 核心区别（Next-Hop、路由传播、AS Path）
   - 节点应该使用 iBGP 还是 eBGP？→ **答案：eBGP**
   - 多节点共享网段的 BGP 处理策略 → **答案：只发布 /32 路由**
   - FRR/Zebra 生产级配置示例
   - **1200+ 行 7 章详解**

3. **[BGP AS 号分配策略](./BGP_AS_ALLOCATION_STRATEGY.md)** 🎯 **架构决策**
   - 如何划分 AS 号？5 种方案对比
   - 一个节点一个 AS（**强烈推荐** ✅）
   - 一个 TOR 一个 AS（备选方案）
   - 其他方案分析
   - AS 号分配算法
   - **860+ 行策略分析**

4. **[AS 号与网段关系](./BGP_AS_AND_SUBNET_RELATIONSHIP.md)** ❓ **常见问题**
   - 每个节点有独立 AS 号，是否需要独立网段？→ **答案：不需要**
   - AS 号 vs 网段：独立概念
   - 为什么可以共享网段（/32 路由原理）
   - 业界实践（Calico、Cilium）
   - **450+ 行深度解析**

#### BGP 文档阅读路径

```
新手路径:
  BGP_NETWORK_DESIGN.md
  → BGP_IBGP_EBGP_ANALYSIS.md (前3章)
  → 开始配置和实验

架构师路径:
  BGP_NETWORK_DESIGN.md
  → BGP_IBGP_EBGP_ANALYSIS.md (完整)
  → BGP_AS_ALLOCATION_STRATEGY.md
  → BGP_AS_AND_SUBNET_RELATIONSHIP.md

故障排查路径:
  BGP_IBGP_EBGP_ANALYSIS.md (第6章)
  → BGP_NETWORK_DESIGN.md (数据包转发部分)
```

### 配置示例

位于 `../configs/` 目录：

#### IPAM 配置
- `daemon.yaml` - IPAM daemon 配置示例
- `cni-config.json` - CNI 插件配置

#### BGP 配置
- `frr-node-example.conf` - FRR/Zebra BGP 配置（**推荐** ✅ 400+ 行生产级配置）
- `bird-node-example.conf` - BIRD BGP 配置（200+ 行）
- `calico-bgp-config.yaml` - Calico BGP 集成配置
- `topology-bgp-example.json` - 拓扑 + BGP 完整配置示例

## 📊 文档概览

| 文档 | 大小 | 类型 | 描述 |
|------|------|------|------|
| DESIGN.md | 342 行 | 架构 | IPAM 核心设计（两层分配 + Raft） |
| ARCHITECTURE.md | 456 行 | 架构 | 拓扑感知架构（v0.3.0） |
| BGP_NETWORK_DESIGN.md | 934 行 | BGP | BGP 网络总体设计 ⭐ |
| BGP_IBGP_EBGP_ANALYSIS.md | 916 行 | BGP | iBGP vs eBGP 深度分析 🔍 |
| BGP_AS_ALLOCATION_STRATEGY.md | 861 行 | BGP | AS 号分配策略 🎯 |
| BGP_AS_AND_SUBNET_RELATIONSHIP.md | 448 行 | BGP | AS 号与网段关系 ❓ |

**总计**: 3,957 行技术文档

## 🗂️ 文档组织原则

1. **分层结构**
   - 根目录：主要文档（README、DESIGN、ARCHITECTURE、BGP 主文档）
   - docs/：辅助文档和深度分析

2. **文档命名**
   - `DESIGN.md` - 设计文档
   - `ARCHITECTURE.md` - 架构文档
   - `BGP_*.md` - BGP 相关文档（按主题命名）

3. **文档关联**
   - 每个文档都包含相关文档引用
   - 避免重复内容，通过链接引用

## 🔍 按主题查找文档

### IP 地址分配
- [DESIGN.md](../DESIGN.md) - 两层分配架构
- [ARCHITECTURE.md](../ARCHITECTURE.md) - 拓扑感知分配

### 网络拓扑
- [ARCHITECTURE.md](../ARCHITECTURE.md) - Zone/Pod/TOR/Node 四层拓扑
- [BGP_NETWORK_DESIGN.md](../BGP_NETWORK_DESIGN.md) - 拓扑与 BGP 集成

### BGP 路由
- [BGP_NETWORK_DESIGN.md](../BGP_NETWORK_DESIGN.md) - BGP 网络设计
- [BGP_IBGP_EBGP_ANALYSIS.md](../BGP_IBGP_EBGP_ANALYSIS.md) - BGP 协议分析

### AS 号规划
- [BGP_AS_ALLOCATION_STRATEGY.md](./BGP_AS_ALLOCATION_STRATEGY.md) - AS 号分配策略
- [BGP_AS_AND_SUBNET_RELATIONSHIP.md](./BGP_AS_AND_SUBNET_RELATIONSHIP.md) - AS 号与网段

### 高可用和共识
- [DESIGN.md](../DESIGN.md) - Raft 共识机制

### 性能优化
- [DESIGN.md](../DESIGN.md) - Bitmap 快速分配
- [ARCHITECTURE.md](../ARCHITECTURE.md) - 本地缓存策略

## 💡 常见问题快速查找

| 问题 | 查看文档 | 章节 |
|------|----------|------|
| 如何实现高性能 IP 分配？ | DESIGN.md | 第 2.1 节（两层分配架构） |
| 如何支持 TOR 级网段管理？ | ARCHITECTURE.md | 第 2 节（网络拓扑模型） |
| 节点应该用 iBGP 还是 eBGP？ | BGP_IBGP_EBGP_ANALYSIS.md | 第 3 章（答案：eBGP） |
| 多节点共享网段如何处理？ | BGP_IBGP_EBGP_ANALYSIS.md | 第 4 章（只发布 /32） |
| 如何分配 AS 号？ | BGP_AS_ALLOCATION_STRATEGY.md | 全文（推荐：每节点一个 AS） |
| 独立 AS 是否需要独立网段？ | BGP_AS_AND_SUBNET_RELATIONSHIP.md | 第 2 节（答案：不需要） |
| 如何配置 FRR BGP？ | BGP_IBGP_EBGP_ANALYSIS.md | 第 5 章 + configs/frr-node-example.conf |
| 如何部署 IPAM 集群？ | Readme.md | 快速开始章节 |

## 🔄 版本演进

- **v0.1.0**: 基础 IPAM（DESIGN.md）
- **v0.2.0**: gRPC + Metrics + IPv6（DESIGN.md）
- **v0.3.0**: 拓扑感知架构（ARCHITECTURE.md）+ BGP 集成（BGP_*.md）

## 📝 文档贡献

欢迎改进文档！请遵循以下原则：

1. **清晰性**: 使用图表和示例
2. **准确性**: 代码示例需可运行
3. **完整性**: 包含背景、设计和实现
4. **关联性**: 引用相关文档

## 🆘 获取帮助

- **Issues**: [GitHub Issues](https://github.com/jianzi123/ipam/issues)
- **Email**: jianzi123@example.com

---

**最后更新**: 2025-11-16
**文档版本**: v0.3.0
