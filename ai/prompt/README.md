# Prompt Engineering

AI 应用工程师的核心技能。Prompt 好不好，直接决定效果和成本。

## 计划收录

### 基础模式
- **Zero-shot**：直接问
- **Few-shot**：给几个示例
- **Chain-of-Thought（CoT）**：让模型"想一想"
- **Self-Consistency**：多次采样投票
- **ReAct**：Reasoning + Acting 交替
- **Tree of Thoughts（ToT）**：思维树
- **Reflexion**：自我反思修正

### 工程技巧
- **System Prompt 设计**：角色、任务、约束、格式
- **结构化输出**：JSON Mode、Function Calling、Grammar 约束
- **上下文压缩**：摘要、Map-Reduce、Refine
- **Prompt 模板化**：变量替换、多语言支持
- **Prompt 版本管理**：像代码一样管理

### 常见问题与优化
- Prompt 注入（Prompt Injection）与防御
- 越狱（Jailbreak）风险
- Token 成本优化
- 输出稳定性（Temperature、Top-P、Seed）
- 多轮对话上下文管理

### 常用模式片段
- 角色扮演模板
- 分类任务模板
- 抽取任务模板（NER、事件抽取）
- 翻译 / 改写 / 摘要模板
- Code 生成 / 修复模板
- Agent 系统提示模板

## 面试常问

- CoT 为什么能提升推理能力？
- Few-shot 示例的顺序、数量有讲究吗？
- 如何让 LLM 稳定输出 JSON？
- Prompt 注入的常见手法和防御方案？
- Prompt 模板如何做版本管理和 AB 测试？
