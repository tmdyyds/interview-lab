# Interview Lab

面试题库 + 技术知识补充 + 框架/类库源码解析。主力语言：**PHP** 和 **Go**。

## 项目定位

- 汇总网上及个人整理的高频面试题与答案
- 记录框架、类库、中间件的源码或使用解析
- 沉淀真实面试实录，形成个人复盘资产
- 长期迭代，作为知识地图使用

## 目录结构

```
interview-lab/
├── php/                # PHP 专题：基础、OOP、框架、生态
├── go/                 # Go 专题：基础、并发、runtime、框架、生态
├── ai/                 # AI 应用层：LLM、Prompt、RAG、Agent、向量库、推理
├── cs-fundamentals/    # 计算机基础：数据结构、算法、网络、操作系统、设计模式
├── databases/          # 数据存储：MySQL、Redis、MongoDB
├── infrastructure/     # 基础设施：MQ、ES、Nginx、Docker、K8s、Linux
├── system-design/      # 系统设计：概念、分布式、实战 case
├── interviews/         # 真实面试实录（按日期归档）
├── inbox/              # 收集夹：随手记，未归档
└── _meta/              # 模板、脚本等元数据
```

## 学习地图

### 语言专题
- [PHP](./php/README.md) — 语法 · OOP · 性能 · Laravel · Hyperf · Swoole
- [Go](./go/README.md) — 语法 · 并发 · GMP · GC · Gin · GORM

### AI 应用
- [AI 应用层](./ai/README.md) — LLM 基础 · Prompt · RAG · Agent · 向量库 · 推理部署

### 通用知识
- [计算机基础](./cs-fundamentals/README.md) — 数据结构 · 算法 · 网络 · OS · 设计模式
- [数据库](./databases/README.md) — MySQL · Redis · MongoDB
  - [Redis 专题](./databases/redis/README.md) — 缓存策略 · 分布式锁 · 持久化 · 哨兵 · 集群 · 12 大业务场景
- [基础设施](./infrastructure/README.md) — MQ · ES · Nginx · Docker · K8s
- [系统设计](./system-design/README.md) — 限流熔断 · 分布式 · 秒杀 · 短链

### 实战与收集
- [面试实录](./interviews/README.md) — 按日期归档的真实面试
- [收集夹](./inbox/README.md) — 随手记，定期归档

## 使用节奏

1. **看到题** → 先丢 `inbox/`，别纠结分类
2. **每周整理** → 从 `inbox/` 归到对应主题目录
3. **面完试** → 一份 `interviews/YYYY-MM-公司-岗位.md`，题分散归主题
4. **复习** → 翻 `php/questions.md` 或 `go/questions.md` 高频题索引

## 检索方式

- **按主题**：直接进对应目录
- **按标签**：编辑器全局搜 `#高频` `#原理` `#手写` `#字节` 等标签
- **按公司**：搜文件名或标签，如 `#alibaba`

## 题目/专题写作模板

- [题目模板](./_meta/templates/question.md)
- [专题模板](./_meta/templates/topic.md)

## 约定

- 目录/文件名：小写英文 + 连字符（`hash-table.md`）
- 内容：中文为主，代码/命令保持原样
- 每个主题目录都有一个 `README.md` 作为该目录的导航
