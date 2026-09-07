# AI 应用框架

## 计划收录

### Python 生态（主流）
- **LangChain**
  - LCEL（LangChain Expression Language）
  - Chain / Agent / Memory / Tool
  - LangGraph：状态图 Agent
  - LangSmith：可观测性平台
- **LlamaIndex**
  - 更专注 RAG
  - Query Engine、Retriever、Node
- **Haystack**：Deepset 出品
- **AutoGen**：微软多 Agent
- **CrewAI**：角色化多 Agent

### Go 生态
- **Eino**：字节开源，主打 Go 侧 LLM 应用
- **langchaingo**：LangChain 的 Go 移植
- **go-openai**：OpenAI 官方风格客户端
- **Ollama Go SDK**：本地模型调用

### PHP 生态
- **openai-php/client**
- **openai-php/laravel**：Laravel 集成
- **Hyperf AI 组件**（社区）

### 低代码 / 平台
- **Dify**：开源 LLMOps 平台
- **Coze**（扣子）：字节
- **FastGPT**：开源知识库
- **n8n / Flowise**：工作流

## 选型考量

- **场景**：纯 RAG → LlamaIndex；复杂 Agent → LangGraph / Eino
- **语言栈**：团队用什么，就用什么生态
- **可观测性**：LangSmith / Langfuse
- **本地化 vs 云**：数据合规、成本
- **社区活跃度**：更新频率、Issue 响应

## 面试常问

- LangChain 和 LlamaIndex 有什么区别？如何选？
- LCEL 相比传统 Chain 有什么优势？
- LangGraph 相比 LangChain Agent 好在哪？
- Eino 的设计理念（DAG、类型安全）
- Go 生态做 AI 应用会遇到哪些坑？
