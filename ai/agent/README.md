# Agent 智能体

让 LLM 从"聊天"变成"能干活"。2024-2026 年 AI 应用的核心方向。

## 计划收录

### 核心能力
- **Function Calling / Tool Use**
  - OpenAI Function Calling
  - Anthropic Tool Use
  - 参数 Schema（JSON Schema）
  - 并行工具调用
- **MCP（Model Context Protocol）**
  - Anthropic 主导的开放协议
  - Server / Client 架构
  - Resources / Tools / Prompts
  - 与 Function Calling 的关系
- **代码执行**：Code Interpreter、Sandbox
- **记忆**：Short-term（上下文）、Long-term（向量库 + 摘要）
- **规划**：任务拆解、DAG、条件分支

### Agent 设计范式
- **ReAct**：Reason + Act 循环
- **Plan-and-Execute**：先规划再执行
- **Reflexion**：执行 + 反思 + 修正
- **Multi-Agent**：
  - CrewAI 风格：角色分工
  - AutoGen 风格：对话协作
  - LangGraph 风格：状态图驱动
- **Supervisor 模式**：主 Agent 调度子 Agent

### 工程挑战
- **可靠性**：失败重试、降级、幂等
- **成本**：Token 消耗爆炸问题
- **延迟**：串行调用的累积延迟
- **可观测性**：Trace、评估、回放
- **安全**：Prompt 注入、工具滥用、权限控制

### 典型应用场景
- 客服机器人
- 代码助手（Cursor、Kiro、Cline）
- 数据分析 Agent
- 浏览器操作 Agent（browser-use、Playwright MCP）
- 工作流自动化（Coze、Dify）

## 面试常问

- Function Calling 底层是怎么实现的？（是模型能力还是外部逻辑？）
- MCP 和 Function Calling 有什么区别？为什么需要 MCP？
- ReAct 的执行流程
- 多 Agent 协作的常见模式
- Agent 的 Token 成本如何控制？
- Agent 长链路失败率高，怎么提升可靠性？
- Agent 的可观测性怎么做？
