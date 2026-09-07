# AI 应用层

AI 工程化视角：如何把 LLM 用到业务里。不深入模型训练（那是算法岗方向），聚焦**应用层落地**。

## 目录

- [fundamentals/](./fundamentals/README.md) — LLM 基础：Transformer、Token、上下文、幻觉
- [prompt/](./prompt/README.md) — Prompt Engineering：CoT、Few-shot、ReAct、结构化输出
- [rag/](./rag/README.md) — 检索增强：Embedding、分块、召回、重排
- [agent/](./agent/README.md) — Agent：Function Calling、MCP、多 Agent 协作
- [frameworks/](./frameworks/README.md) — 应用框架：LangChain、LlamaIndex、Eino、Dify
- [vector-db/](./vector-db/README.md) — 向量数据库：Milvus、Qdrant、pgvector
- [inference/](./inference/README.md) — 推理部署：vLLM、Ollama、TGI
- [evaluation/](./evaluation/README.md) — 效果评估：RAGAs、幻觉检测、AB 测试
- [questions.md](./questions.md) — 高频面试题索引

## 学习路径建议

1. **基础概念**：`fundamentals/` — Token、上下文窗口、温度、Top-P
2. **Prompt 工程**：`prompt/` — 是 AI 应用工程师的核心技能
3. **RAG 实战**：`rag/` — 目前落地最广的方向
4. **Agent 进阶**：`agent/` — Function Calling → MCP → 多 Agent
5. **工程化**：`inference/` 部署 + `evaluation/` 评估闭环

## 常考重点

- Transformer 核心机制（Self-Attention、位置编码）
- Token 计算与上下文窗口管理
- Prompt Engineering 常用技巧
- RAG 完整链路（分块 → 向量化 → 召回 → 重排 → 生成）
- 向量检索算法（HNSW、IVF、PQ）
- Function Calling 与 MCP 协议
- Agent 设计范式（ReAct、Plan-and-Execute、Reflexion）
- LLM 应用的幻觉、成本、延迟控制
- LangChain vs LlamaIndex vs Eino 选型

## PHP / Go 视角

- **Go**：Eino（字节）、langchaingo、go-openai、Ollama Go SDK
- **PHP**：openai-php/client、Hyperf 生态 AI 组件
- 大部分 AI 生态用 Python，但**应用层完全可以用 Go/PHP 调 API 或本地模型**
