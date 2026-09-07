# Linux

## 计划收录

### 常用命令
- 文本处理：grep、awk、sed、cut、sort、uniq、wc
- 文件搜索：find、locate、which、whereis
- 权限：chmod、chown、umask
- 进程：ps、top、htop、kill、nohup
- 网络：netstat、ss、lsof、tcpdump、curl、wget

### 性能排查工具
- **CPU**：top、mpstat、pidstat、perf
- **内存**：free、vmstat、pmap
- **磁盘**：iostat、iotop、du、df
- **网络**：iftop、nethogs、ss、tcpdump
- **综合**：dstat、sar
- **进程追踪**：strace、ltrace
- **火焰图**：perf + FlameGraph

### 排查思路
- Load average 高 → CPU / IO / 上下文切换
- 内存 OOM → dmesg、oom-killer
- 磁盘 IO 高 → iostat + iotop
- 网络问题 → tcpdump + Wireshark
- 应用无响应 → strace + jstack/pprof

## 面试常问

- 如何排查 CPU 100%？
- 如何排查内存泄漏？
- 如何统计某端口的连接数？
- awk 一行统计 Nginx 日志各状态码数量
