# 向量数据库

RAG 落地的核心存储层。

## 计划收录

### 主流产品对比
- **Milvus / Zilliz**：功能全、部署重、社区大
- **Qdrant**：Rust 编写、性能好、易部署
- **Weaviate**：内置多模态、GraphQL
- **Chroma**：轻量、开发友好、Embedded 模式
- **Pinecone**：全托管 SaaS
- **pgvector**：PostgreSQL 扩展，融合关系 + 向量
- **Elasticsearch 8.x**：支持 dense_vector + kNN
- **Redis Stack**：RediSearch 向量能力

### 核心索引算法
- **HNSW**（Hierarchical Navigable Small World）
  - 目前工业界主流
  - 参数 M、efConstruction、ef
- **IVF**（Inverted File Index）+ 变体（IVF-Flat、IVF-PQ、IVF-SQ）
- **PQ**（Product Quantization）：压缩存储
- **DiskANN**：SSD 友好
- **Annoy**（Spotify）、**ScaNN**（Google）

### 距离度量
- Cosine Similarity（余弦）
- L2 / Euclidean（欧氏）
- Inner Product（内积）
- Hamming（二值向量）

### 工程要点
- **Recall vs Latency vs Cost** 三角权衡
- 索引构建时间与增量更新
- 元数据过滤（Filtered Search）
- 混合检索（向量 + 关键词）
- 分片、副本、多租户
- 冷热数据分层

## 面试常问

- 向量数据库和传统数据库的区别？
- HNSW 原理简述？为什么比 IVF 好？
- IVF-PQ 如何在精度和存储间权衡？
- 亿级向量如何做水平扩展？
- 元数据过滤和向量检索的执行顺序（Pre-filter vs Post-filter）
- pgvector 能替代专用向量库吗？什么场景可以？
