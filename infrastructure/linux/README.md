# Linux

Linux 后端 / SRE 面试的核心内容——**命令行 + 性能排查 + 生产事故处理**。四份文档形成完整体系：**命令基础 → 性能工具 → 排查流程 → 面试题**。

## 已收录

- [**commands.md**](./commands.md) — 常用命令实战
  - 文本处理：grep / awk / sed / cut / sort / uniq / wc
  - 文件搜索：find / locate / which / whereis
  - 权限管理：chmod / chown / umask
  - 进程管理：ps / top / kill / nohup / pkill
  - 网络工具：ss / lsof / tcpdump / curl / wget
  - 每条命令带 flag 注释 + 真实输出示例 + 组合技锦囊

- [**perf-tools.md**](./perf-tools.md) — 性能排查工具箱
  - **CPU**：mpstat / pidstat / perf / top
  - **内存**：free / vmstat / pmap / /proc/meminfo
  - **磁盘**：iostat / iotop / du / df
  - **网络**：iftop / nethogs / ss / tcpdump
  - **综合**：dstat / sar（历史数据回溯）
  - **进程追踪**：strace / ltrace
  - **火焰图**：perf + FlameGraph 完整流程
  - 每个工具带字段解读 + 输出示例 + 判读心法

- [**troubleshooting.md**](./troubleshooting.md) — 生产问题排查流程
  - Load average 高（CPU / IO / 上下文切换分类判断）
  - OOM / 内存耗尽（dmesg + oom-killer + cgroup）
  - 磁盘 IO 高（iostat 判读 + iotop 定位）
  - 网络异常（TIME-WAIT / CLOSE-WAIT / SYN queue）
  - 应用无响应 / 卡死（strace + pprof + jstack）
  - USE 方法 + Brendan Gregg 60 秒诊断法

- [**interview-questions.md**](./interview-questions.md) — 面试高频题
  - **核心必背 4 题**：CPU 100% / 内存泄漏 / 端口连接数 / awk 日志统计
  - Part 2-7 共 20 道扩展题：命令行手感、Load、僵尸进程、软硬链接、TCP、DNS、抓包
  - 每题带完整命令 + 逐段拆解 + 常见追问

## 阅读顺序建议

```
入门/温故：     commands.md      （熟悉命令）
性能面试：     perf-tools.md     （了解每个工具）
生产实战：     troubleshooting.md （建立排查思维）
面前突击：     interview-questions.md （核心题必背）
```

## 面试核心主题

绝大部分深度问题都会绕回这四个：

1. **性能排查思路**（60 秒诊断法 + USE 方法）
2. **系统调用 / 内核态**（strace 用法、%sys 高怎么办）
3. **网络连接状态**（TIME-WAIT / CLOSE-WAIT / SYN queue 都是必问）
4. **日志分析一行流**（awk + sort + uniq 组合技）

## 常问一梯队

- 如何排查 CPU 100%？如何排查内存泄漏？
- Load average 高但 CPU 不忙，怎么回事？（→ D 状态进程 / iowait）
- awk 一行统计 Nginx 日志的状态码 / TOP URL / TOP IP
- TIME-WAIT 和 CLOSE-WAIT 堆积分别是什么问题？怎么修？
- 软链接 vs 硬链接？删除源文件后哪个还能用？
- `kill -9` 为什么不能被捕获？（→ SIGKILL 与 SIGSTOP 由内核直接处理）
- 磁盘满了但 `df` 显示还有空间？（→ inode / 被删但未释放的 fd）
- 抓包怎么快速定位是网络还是应用问题？
