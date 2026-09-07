# LLM 基础

面试常问的模型侧基础概念。不用会推公式，但要能讲清原理和影响。

## 计划收录

### 模型架构
- Transformer 核心：Self-Attention、Multi-Head、位置编码
- Encoder-only（BERT）vs Decoder-only（GPT）vs Encoder-Decoder（T5）
- 主流模型：GPT、Claude、Llama、Qwen、DeepSeek、GLM
- MoE（Mixture of Experts）架构

### 核心概念
- **Token 与 Tokenizer**：BPE、WordPiece、SentencePiece
- **上下文窗口**（Context Window）：8K → 128K → 1M+ 的演进
- **参数量**：7B / 13B / 70B / 175B 意味着什么
- **推理参数**：
  - Temperature（温度）
  - Top-P（核采样）
  - Top-K
  - Frequency / Presence Penalty
  - Stop Sequences
- **Prompt / Completion / Chat 模板**：System / User / Assistant

### 训练阶段
- Pretraining（预训练）
- SFT（Supervised Fine-Tuning，监督微调）
- RLHF / DPO（人类反馈强化学习）
- 微调方式：Full FT / LoRA / QLoRA / P-Tuning

### 常见问题
- **幻觉**（Hallucination）：原因与缓解
- **长文本**：位置编码扩展（RoPE、ALiBi、YaRN）
- **对齐**（Alignment）与安全（Safety）

## 面试常问

- Transformer 为什么比 RNN 好？
- Self-Attention 的计算复杂度是多少？
- 为什么现在主流是 Decoder-only？
- Token 是什么？中英文 Token 消耗差异？
- Temperature 越高越好吗？什么场景设 0？
- LoRA 为什么能大幅降低微调成本？
- 幻觉是怎么产生的？如何缓解？
