# RAG（检索增强生成）

Retrieval-Augmented Generation，目前 LLM 落地最主流的方式。

## 计划收录

### 完整链路
```
数据源 → 加载 → 清洗 → 分块 → Embedding → 向量库
                                                    ↓
用户 Query → 改写/扩展 → 向量化 → 召回 → 重排 → 拼 Prompt → LLM → 回答
```

### 关键环节
- **数据加载**：PDF、Word、Markdown、HTML、结构化数据
- **分块（Chunking）策略**：
  - 固定长度
  - 按句/段落
  - 递归分割（RecursiveCharacterTextSplitter）
  - 语义分块（Semantic Chunking）
  - 父子块（Parent-Child）
- **Embedding 模型**：
  - OpenAI text-embedding-3
  - BGE、M3E（中文）
  - jina-embeddings
  - 本地部署选型
- **向量检索**：见 [../vector-db/](../vector-db/README.md)
- **召回策略**：
  - 单路：纯向量
  - 多路：向量 + BM25（关键词）+ 元数据过滤
  - HyDE（假设文档嵌入）
  - Query 改写 / 拆分
- **重排（Rerank）**：Cohere Rerank、bge-reranker、Cross-Encoder
- **Prompt 拼接**：上下文长度控制、引用格式
- **答案生成**：结合上下文、幻觉抑制、引用溯源

### 进阶模式
- **Advanced RAG**：Query Transformation、Sentence Window、Auto-Merging
- **Multi-Modal RAG**：图片、表格、图表
- **GraphRAG**：知识图谱 + LLM（微软开源）
- **Agentic RAG**：Agent 自主决定检索

### 评估
- 检索指标：Recall@K、MRR、NDCG
- 生成指标：Faithfulness、Answer Relevance、Context Precision
- 工具：RAGAs、TruLens、DeepEval

## 面试常问

- RAG 完整链路讲一遍
- 分块大小怎么定？块太大/太小各有什么问题？
- 为什么要重排？单向量召回不够吗？
- 混合检索（向量 + BM25）如何做融合？（RRF）
- 长文档如何做 RAG？（Parent-Child、Sentence Window）
- RAG 幻觉问题如何缓解？
- 如何评估一个 RAG 系统的好坏？
- Fine-tune vs RAG 如何选？
