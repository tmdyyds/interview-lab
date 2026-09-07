# Go Runtime 深度剖析

**标签**: #go #runtime #internals #高频

Go runtime 是让 Go 从"简单语法"变成"高并发神器"的核心。**几乎所有中高级 Go 面试都会问 runtime**——GMP、GC、内存分配、逃逸分析这四座大山绕不开。

本目录用**源码级别 + 图示 + 版本演进 + 面试题**的方式讲透。

---

## 📚 索引

### 🔴 一梯队 · 必考

| 主题 | 覆盖内容 | 难度 |
|-----|---------|------|
| [01. GMP 调度器](./01-gmp-scheduler.md) | G/M/P 结构、work stealing、抢占式调度、netpoller | ⭐⭐⭐⭐ |
| [02. GC 与写屏障](./02-gc-and-write-barrier.md) | 三色标记、混合写屏障、GC 触发时机、STW 演进 | ⭐⭐⭐⭐⭐ |
| [03. 内存分配器](./03-memory-allocator.md) | mcache/mcentral/mheap、Size Class、大对象分配 | ⭐⭐⭐⭐ |
| [04. 逃逸分析](./04-escape-analysis.md) | 栈 vs 堆、逃逸规则、优化技巧 | ⭐⭐⭐ |

### 🟡 二梯队 · 大厂高频（待完成）

- 05. Goroutine 栈机制（连续栈、扩容、收缩）
- 06. Channel 底层实现（hchan、直接传递、select 随机化）
- 07. sync 底层（Mutex 饥饿模式、Pool victim cache、Map 双 map）

### 🟢 三梯队 · 加分项（待完成）

- 08. Interface iface/eface（itab、类型断言性能）
- 09. defer 演进（堆分配 → 栈分配 → open coded）
- 10. 反射与 unsafe（三定律、四合法转换、`//go:linkname`）

---

## 学习顺序

**推荐顺序**：4 → 3 → 1 → 2

- **逃逸分析**是"变量在栈还是堆"的入门，先理解这个
- **内存分配器**讲清楚"在堆上怎么分配"
- **GMP 调度器**讲清楚"goroutine 怎么跑"
- **GC** 是最难的，需要前面几个铺垫

面试反过来：**先问 GMP → 再问 GC**（这两个是重头戏）。

---

## 版本坐标

Go runtime 演进很快，面试常问"XX 是从哪个版本引入的"。关键版本：

| 版本 | 里程碑 |
|-----|-------|
| Go 1.1 | 引入 GMP 模型 |
| Go 1.3 | 分段栈 → 连续栈 |
| Go 1.5 | 三色标记 GC，STW 从秒级到毫秒级 |
| Go 1.8 | 混合写屏障（Yuasa + Dijkstra） |
| Go 1.13 | defer 栈分配 |
| Go 1.14 | 基于信号的**抢占式调度**（终结协作式） |
| Go 1.14 | defer open coded（内联到函数尾） |
| Go 1.18 | 泛型（不影响 runtime 核心） |
| Go 1.19 | `GOMEMLIMIT` 软内存上限 |
| Go 1.20 | goroutine 抢占优化、CGo 性能优化 |
| Go 1.21 | PGO（Profile-guided Optimization） |
| Go 1.22 | 循环变量 per-iteration 语义 |
| Go 1.25 | 容器 CGroup CPU 感知（终于原生支持） |

---

## 阅读建议

每篇文档结构统一：

1. **是什么**（3 句话说清核心）
2. **底层数据结构**（源码 struct 定义）
3. **核心机制**（流程图 + 代码演进）
4. **性能剖析**（为什么这么设计）
5. **版本演进**（不同 Go 版本的差异）
6. **面试高频题**（含深度追问）

**别死记源码**，理解**"为什么这样设计"和"这个设计解决什么问题"**才是重点。
