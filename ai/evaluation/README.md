# 效果评估

LLM 应用没有评估等于没有测试。这是工程化和玩具的分水岭。

## 计划收录

### 评估维度
- **通用能力**：MMLU、C-Eval、GSM8K、HumanEval
- **RAG 效果**：
  - Faithfulness（忠实度，是否根据上下文）
  - Answer Relevance（回答相关性）
  - Context Precision / Recall（上下文精度/召回）
  - Context Relevance
- **Agent 效果**：任务完成率、工具调用正确率、步数、成本
- **业务指标**：转化率、满意度、拒答率、幻觉率

### 评估方法
- **Ground Truth 对比**：BLEU、ROUGE、Exact Match
- **LLM as Judge**：用更强的模型评分
- **人工评审**：金标准，成本高
- **AB 测试**：线上实验
- **红队测试**（Red Teaming）：安全 / 越狱

### 工具生态
- **RAGAs**：RAG 专项评估
- **DeepEval**：pytest 风格
- **TruLens**：可观测 + 评估
- **LangSmith**：LangChain 官方
- **Langfuse**：开源可观测
- **PromptFoo**：Prompt AB 测试
- **OpenAI Evals**

### 可观测性（Observability）
- **Trace**：完整调用链（Prompt / Tool / Retrieval / Response）
- **成本追踪**：Token 消耗、模型分布
- **延迟分布**：P50 / P95 / P99
- **异常告警**：错误率、幻觉率突增

## 面试常问

- RAG 系统怎么评估？只看 BLEU 够吗？
- LLM as Judge 有什么问题？（一致性、偏见）
- 幻觉如何量化？
- Agent 长链路怎么定位失败点？
- Prompt 修改前后怎么验证效果？
- 线上 LLM 应用要监控哪些指标？
