# AI 应用层高频面试题索引

按主题归类，新增题目时同步更新本索引。

## 基础

- [ ] Transformer 相比 RNN / CNN 的优势
- [ ] Self-Attention 计算过程和复杂度
- [ ] Encoder-only / Decoder-only / Enc-Dec 的适用场景
- [ ] Token 是什么？为什么中文 Token 消耗更高？
- [ ] 上下文窗口越长越好吗？
- [ ] Temperature、Top-P、Top-K 分别控制什么？
- [ ] LoRA 微调原理与优势
- [ ] SFT 和 RLHF 的区别
- [ ] 幻觉产生的根本原因

## Prompt

- [ ] Chain-of-Thought 为什么有效？
- [ ] Few-shot 的样本选择有讲究吗？
- [ ] 如何让模型稳定输出 JSON？
- [ ] System Prompt 和 User Prompt 的差异
- [ ] Prompt 注入攻击原理与防御
- [ ] Prompt 版本管理和 AB 测试怎么做

## RAG

- [ ] 完整讲一遍 RAG 链路
- [ ] 分块策略如何选？块的大小影响什么？
- [ ] Embedding 模型如何选？中英文混合怎么办？
- [ ] 为什么需要 Rerank？
- [ ] 混合检索（向量 + BM25）的融合算法（RRF）
- [ ] HyDE 是什么？什么场景有效？
- [ ] Parent-Child、Sentence Window 分别解决什么问题？
- [ ] GraphRAG 相比传统 RAG 的差异
- [ ] Fine-tune vs RAG 如何选？

## Agent

- [ ] Function Calling 底层原理
- [ ] MCP 协议解决了什么问题？和 Function Calling 什么关系？
- [ ] ReAct 的执行流程
- [ ] Plan-and-Execute vs ReAct 的差异
- [ ] 多 Agent 协作有哪些模式？
- [ ] Agent 长链路失败率高怎么办？
- [ ] Agent 的 Token 成本如何控制？
- [ ] Agent 的可观测性怎么做？

## 向量数据库

- [ ] 向量数据库为什么不能用普通数据库替代？
- [ ] HNSW 原理和关键参数
- [ ] IVF-PQ 的原理和适用场景
- [ ] Pre-filter vs Post-filter
- [ ] 亿级向量如何存储和检索？
- [ ] pgvector 能替代 Milvus 吗？

## 推理部署

- [ ] KV Cache 的作用和显存占用
- [ ] vLLM 为什么快？（PagedAttention + Continuous Batching）
- [ ] 量化方案对比（INT8 / INT4 / AWQ / GPTQ）
- [ ] 一个 7B 模型部署大约需要多少显存？
- [ ] 流式输出（SSE）如何实现？
- [ ] 多模型路由（one-api / LiteLLM）解决什么问题？

## 评估与工程

- [ ] RAG 系统的评估指标
- [ ] LLM as Judge 的问题
- [ ] 幻觉如何量化和监控？
- [ ] LLM 应用的核心可观测指标
- [ ] 如何做 Prompt 的 AB 测试？
- [ ] 线上 LLM 应用的常见故障及应对

## 场景 / 系统设计

- [ ] 设计一个企业内知识库问答系统
- [ ] 设计一个代码 Review Agent
- [ ] 设计一个客服机器人（含转人工）
- [ ] 设计一个 AI 数据分析 Agent
- [ ] 亿级文档的 RAG 系统如何做？
