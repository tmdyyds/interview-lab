# 模型推理与部署

如何把模型跑起来。应用层工程师要懂原理，不一定要自己训练。

## 计划收录

### 推理框架
- **vLLM**：PagedAttention、Continuous Batching，生产首选
- **TGI**（Text Generation Inference）：HuggingFace 出品
- **SGLang**：性能强，RadixAttention
- **Ollama**：本地开发/演示利器
- **llama.cpp**：CPU / 边缘设备
- **LMDeploy**：InternLM 团队
- **TensorRT-LLM**：NVIDIA 官方极致优化

### 推理优化技术
- **KV Cache**：为什么必须缓存
- **PagedAttention**：vLLM 的核心优化
- **Continuous Batching**：吞吐量翻倍
- **Speculative Decoding**（推测解码）
- **量化**：INT8、INT4、AWQ、GPTQ、GGUF
- **Flash Attention**：显存 + 速度双优化
- **张量并行 / 流水线并行**

### 部署形态
- **API 网关**：OpenAI 兼容协议是事实标准
- **多模型路由**：LiteLLM、one-api
- **本地部署 vs 云 API 权衡**：
  - 数据合规
  - 成本模型
  - 延迟 / 稳定性
  - 模型能力上限
- **GPU 选型**：A100 / H100 / A10 / L20 / 4090

### 工程要点
- 显存估算：模型大小 + KV Cache
- QPS / 并发 / P99 延迟
- 流式响应（SSE / WebSocket）
- 熔断限流（LLM API 昂贵且不稳定）
- 多副本负载均衡

## 面试常问

- KV Cache 是什么？为什么必须要？
- vLLM 相比 HuggingFace Transformers 为什么快这么多？
- Continuous Batching 和 Static Batching 的区别？
- 量化会损失多少精度？INT4 还能用吗？
- 一个 7B 模型部署大概需要多少显存？
- OpenAI 兼容协议为什么成了事实标准？
