# Linux 生产问题排查流程

**标签**: #linux #troubleshooting #sre #高频

**问题现象 → 判断类别 → 定位根因 → 修复**。本文按 5 类典型问题给出决策树、命令、真实案例。配合 [commands.md](./commands.md) 和 [perf-tools.md](./perf-tools.md) 使用。

---

## 目录

1. [排查大原则](#一排查大原则)
2. [Load Average 高](#二load-average-高)
3. [OOM / 内存耗尽](#三oom--内存耗尽)
4. [磁盘 IO 高](#四磁盘-io-高)
5. [网络异常](#五网络异常)
6. [应用无响应 / 卡死](#六应用无响应--卡死)
7. [USE 方法与 60 秒诊断](#七use-方法与-60-秒诊断)

---

## 一、排查大原则

### 三层思维

```
应用层        →  日志 / 业务指标 / pprof / jstack
运行时/框架层  →  Go runtime / JVM / Node event loop
系统层        →  CPU / 内存 / 磁盘 / 网络
```

**从上往下排查**通常最快——业务日志能告诉你 "在 handleOrder 里卡了"，然后再看系统指标验证。

### 判断优先级：SLI 出问题最先看这些

```
Latency 高   →  CPU / IO / 锁 / 依赖服务
Error 率高   →  日志 / 重启 / 依赖服务
Traffic 异常 →  网络 / 上游 / DDOS
Saturation   →  CPU / 内存 / 磁盘 / 网络饱和度
```

### 采集顺序

**先量化，再定位**——不要一上来就 strace / pprof。

```
1. uptime          → 看 load
2. dmesg | tail    → 看内核有没有喊
3. vmstat 1        → CPU 大方向 + 内存换页
4. mpstat -P ALL 1 → CPU 单核分布
5. pidstat 1       → 进程级
6. iostat -xz 1    → 磁盘
7. free -h         → 内存
8. sar -n DEV 1    → 网络
```

**Brendan Gregg 的 60 秒诊断法**，见第七节。

---

## 二、Load Average 高

### 现象特征

```bash
uptime
# 10:30:15 up 30 days, load average: 15.20, 12.85, 8.60
#                                    ↑ 1min  5min  15min
```

**判断标准**：`load average / CPU 核数 > 1` 说明系统满载。

看 `nproc` 得到核数：`nproc # 输出 8`。上例 8 核负载 15 = **1.87 倍过载**。

**三个数字的含义**：
- `1min > 5min > 15min` → 突然飙升（找刚才发生的事）
- `1min < 5min < 15min` → 正在缓解
- 三个都高且稳定 → 持续压力

### 决策树

```
load 高
  │
  ├─ %usr 高 (mpstat)          → 应用 CPU 密集 → perf/pprof
  │
  ├─ %sys 高                    → 内核态多 → 系统调用/上下文切换
  │    └─ vmstat cs 列高        → 锁竞争或 GOMAXPROCS 设置有问题
  │
  ├─ %iowait 高                 → 等磁盘 → iostat -x
  │
  ├─ %soft 高                   → 软中断多 → 网络包处理（tcpdump/ethtool -S）
  │
  └─ %idle 高但 load 仍然高      → 有大量 D 状态进程（不可中断睡眠）
       └─ ps aux | awk '$8~/D/'  → 通常是等磁盘 IO
```

### 命令流程

```bash
# 1. 快速概览
uptime && nproc

# 2. 各类 CPU 时间分布
mpstat -P ALL 1 3
# 看 %usr / %sys / %iowait / %soft 哪个高

# 3. 找到 CPU 高的进程
pidstat 1 3

# 4. 看被动上下文切换（判断是不是 CPU 争抢）
pidstat -w 1

# 5. 找 D 状态进程（等磁盘卡死）
ps aux | awk '$8 ~ /D/ {print}'
# 输出示例：
# mysql   1234 0.5 5.2 ... D  10:00 0:15 mysqld

# 6. D 状态卡在哪个 syscall
sudo cat /proc/1234/stack
# 输出：[<0>] io_schedule ... [<0>] xfs_ilock ...  ← 磁盘 IO 卡死
```

### 常见根因与修复

| 根因 | 关键信号 | 修复 |
|-----|---------|------|
| 应用 CPU 密集 | `%usr` 高 | 优化算法、加缓存、分布式 |
| 大量小对象分配 | `%sys` 高 + GC 频繁 | `sync.Pool` / 预分配 |
| 锁竞争 | `cs` 高 + `%sys` 高 | 拆锁、无锁数据结构 |
| 慢磁盘 | `%iowait` 高 + `D` 进程多 | 上 SSD、加缓存、批量写 |
| 中断风暴 | `%soft` 高 | RSS/RPS 调整、offload 到网卡 |
| 云主机资源争抢 | `%steal` 高 | 换独占实例或调大规格 |
| Go GOMAXPROCS 过大 | 上下文切换极多 | 用 `uber-go/automaxprocs` |

### 真实案例：某 Go 服务 load 突增

```bash
# 现象：uptime 显示 load = 40，8 核机器
$ uptime
 10:30:15 up 5 days, load average: 40.20, 35.85, 20.60

# 第一步：看 CPU 分布
$ mpstat -P ALL 1 3
Average: CPU  %usr  %sys  %iowait  %idle
Average: all  15.0  75.0    2.0    8.0
                    ↑ 系统态极高！

# 第二步：看上下文切换
$ vmstat 1 3
 r  b   ...  in    cs
30  0        1200  180000
                   ↑ 18 万/秒，远超正常（5-10 万）

# 第三步：定位进程
$ pidstat -w 1
PID   cswch/s  nvcswch/s  Command
1234  50000    150000     go-service     ← 就是它

# 第四步：进 pprof
$ go tool pprof http://localhost:6060/debug/pprof/mutex
(pprof) top
Showing nodes 3s of 3s total
    2.8s  sync.(*Mutex).Lock

# 根因：某段代码在热路径持全局锁。改用 sharding lock 后 load 降回 5。
```

---

## 三、OOM / 内存耗尽

### 现象特征

- 应用突然被"杀掉"（无日志、退出码 137 或 -9）
- `dmesg` 或 `/var/log/messages` 有 `Out of memory: Killed process`
- `free -h` 显示 `available` 接近 0，`Swap used` 涨

### 决策树

```
应用被杀
  │
  ├─ dmesg 显示 oom-killer     → 内核 OOM Kill
  │    └─ 系统内存不够，进程被自动挑一个杀
  │
  ├─ available 内存持续下降     → 有泄漏
  │    └─ pmap / go pprof heap → 定位泄漏源
  │
  ├─ Swap 满 + si/so 高         → 内存严重不够，性能崩塌
  │
  └─ cgroup 限制被触发（容器）    → 容器 OOM，不是宿主机
       └─ cat /sys/fs/cgroup/memory/memory.oom_control
```

### 命令流程

```bash
# 1. 看内核日志有没有 OOM
sudo dmesg -T | grep -i "out of memory"
# 或
sudo grep -i "killed process" /var/log/syslog

# 输出：
# [Sep 22 10:30:15] Out of memory: Killed process 1234 (java) total-vm:8000000kB, anon-rss:6000000kB
#                                                              ↑ 虚拟内存    ↑ 匿名 RSS = 堆

# 2. 全局内存概览
free -h

# 3. 找内存大户
ps aux --sort=-%mem | head -10
#      ↑ 按内存降序

# 或按 RSS 精确排
ps -eo pid,user,rss,vsz,comm --sort=-rss | head -10

# 4. 某进程的内存分布
pmap -x 1234 | tail -1        # 看总量
sudo cat /proc/1234/status | grep -E "VmRSS|VmSize|VmSwap"
# VmRSS:    6000000 kB    ← 物理内存
# VmSize:   8000000 kB    ← 虚拟内存
# VmSwap:         0 kB    ← swap 使用

# 5. 内核 slab 缓存（内核对象泄漏时看这个）
sudo slabtop
```

### 容器 OOM 判定

```bash
# 容器内查 cgroup 限制
cat /sys/fs/cgroup/memory/memory.limit_in_bytes
# 4294967296     ← 4G 限制

cat /sys/fs/cgroup/memory/memory.usage_in_bytes
# 4200000000    ← 4.2G，快撞上限了

# K8s 里通过 events 看
kubectl describe pod xxx | grep -A 5 "OOMKilled"
# Last State:     Terminated
#   Reason:       OOMKilled
#   Exit Code:    137
```

### 常见根因与修复

| 根因 | 关键信号 | 修复 |
|-----|---------|------|
| 应用堆持续增长 | RSS 只涨不降 | pprof heap 定位泄漏 |
| 全局 map/slice 只加不删 | 特定数据结构占 90% | 加 TTL / LRU |
| 大对象缓存无淘汰 | Cache 占大头 | 用 groupcache / ristretto |
| goroutine 泄漏带走栈 | numGoroutines 涨 | pprof goroutine |
| 短时间大流量 | 突发高分配 | 加限流 / 缩短请求超时 |
| JVM 堆过大 | -Xmx 配置错 | 调 heap 大小 |
| 容器 limit 太紧 | RSS 未涨异常但被 killed | 提 limit 或优化内存 |

### 真实案例：K8s Pod 反复 OOMKilled

```bash
# 现象：Pod 每 20 分钟被 kill 重启
$ kubectl get pods
NAME              READY   STATUS       RESTARTS   AGE
app-abc123        0/1     OOMKilled    12         4h

# 1. 确认是 OOM 而不是 crash
$ kubectl describe pod app-abc123 | grep -A 3 State
State:  Waiting
  Reason:  CrashLoopBackOff
Last State: Terminated
  Reason: OOMKilled
  Exit Code: 137

# 2. 进入容器（如果还活着）拉 pprof
$ kubectl port-forward app-abc123 6060:6060
$ go tool pprof http://localhost:6060/debug/pprof/heap
(pprof) top
Showing nodes accounting for 800MB, 90.9% of 880MB total
    500MB  cache.(*LocalCache).Set     ← 本地缓存吃了 500M
    200MB  http.Server.readRequest    ← 请求体缓冲
     50MB  ...

# 3. 定位到 cache 无 TTL 且无上限
# 修复：加 TTL + 最大条数限制
cache := ristretto.NewCache(&ristretto.Config{
    NumCounters: 1e7,        // ★ 追踪 1000 万个 key
    MaxCost:     512 << 20,  // ★ 最多用 512MB
    BufferItems: 64,
})

# 4. 加大 limit 作短期缓解
# resources.limits.memory: 1Gi → 2Gi
```

---

## 四、磁盘 IO 高

### 现象特征

- 应用响应变慢，CPU 却不高
- `%iowait` 很高（> 30%）
- `iostat` 里 `%util` 接近 100%
- 有 `D` 状态进程

### 决策树

```
IO 慢
  │
  ├─ %util 高 + await 高       → 磁盘饱和
  │    └─ iotop 找到罪魁进程
  │
  ├─ %util 高 + await 低       → 队列深但每次快（正常高并发）
  │
  ├─ IOPS 大量小 IO            → 频繁 fsync / 小文件
  │    └─ 换成批量写 / 攒 buffer
  │
  ├─ 顺序读写但慢               → 磁盘老 / 网络存储瓶颈
  │
  └─ inode 满                  → df -i 检查
```

### 命令流程

```bash
# 1. 快速看磁盘状态
iostat -xz 1 3
# 关注：%util、await、r/s、w/s、rrqm/s、wrqm/s

# 2. 找到 IO 大户
sudo iotop -oP
# 或
sudo iotop -oPa
#          ↑ -a 累计（更适合找长期 IO 高的进程）

# 3. 定位进程在读/写什么文件
sudo lsof -p 1234 | grep -E "REG|DIR"
#                        ↑ 只看普通文件和目录

# 4. 跟踪某进程的文件 syscall
sudo strace -e trace=open,read,write,fsync -p 1234
# 看它在操作哪些文件、多久 fsync 一次

# 5. 磁盘使用（先看整体再定位）
df -h
df -i           # inode 用量

# 6. 找大文件
sudo find / -type f -size +1G 2>/dev/null

# 7. 找变化最快的文件（谁在猛写）
sudo find /var/log -mmin -5 -size +10M -ls
#                  ↑ 5 分钟内改过    ↑ 大于 10MB
```

### 常见根因与修复

| 根因 | 关键信号 | 修复 |
|-----|---------|------|
| 日志狂写 | 某进程 write 大 | 降级日志、异步日志、限速 |
| 数据库频繁 fsync | fsync 多、await 高 | 攒批、调 innodb_flush_log_at_trx_commit |
| 全表扫 / 慢 SQL | mysqld 读盘大 | 加索引、优化 SQL |
| 临时文件写满 /tmp | df 报警 | 清理 / 限制 tmpfs |
| 小文件海量 | rrqm/wrqm 低（无合并） | 归档 / 合并小文件 |
| RAID 重建 / 卡故障 | dmesg 报错 | 换盘 / 修 RAID |
| 网络存储（NFS）慢 | 读写延迟高但本地盘空闲 | 检查网络 |

### 真实案例：MySQL 变慢

```bash
# 现象：MySQL QPS 下降 50%，CPU 才 20%

# 1. 看 iowait
$ mpstat 1 3
Average: %usr %sys %iowait %idle
         15    3    50      32
                     ↑ iowait 50% 是大问题

# 2. 找磁盘
$ iostat -xz 1 3
Device  r/s   w/s   %util  await
sda    500   1500   95.0   45.2     ← 满了

# 3. 谁在跑
$ sudo iotop -oPa
PID  DISK READ  DISK WRITE  COMMAND
1234  2 MB/s    30 MB/s     mysqld
5678  0.1 MB/s  50 MB/s     rsyslogd   ← ⚠️ 日志

# 4. 日志狂写查原因
$ sudo strace -e trace=write -p 5678 2>&1 | head
write(4, "Sep 22 10:30 error ..."..., 128)
write(4, "Sep 22 10:30 error ..."..., 128)
# 每秒几千行 error，是应用在疯狂报错日志

# 5. 定位应用 error 源，修复根因；同时限日志速率
# rsyslog.conf 加 rate-limit：
# $SystemLogRateLimitInterval 10
# $SystemLogRateLimitBurst 500
```

---

## 五、网络异常

### 现象特征

- 接口超时、连接被拒绝、握手失败
- `ss` 显示大量 `TIME-WAIT` 或 `CLOSE-WAIT`
- ping 通但业务不通
- 抓包看到大量 `RST` / 重传

### 决策树

```
网络问题
  │
  ├─ 完全不通                   → 网络层（路由/防火墙/网卡）
  │    ├─ ping / traceroute
  │    ├─ ip route / ip a
  │    └─ iptables -L / firewalld
  │
  ├─ 能连但慢                   → 应用层慢查询 or 网络延迟
  │    ├─ mtr / traceroute → 判断哪一跳延迟高
  │    └─ tcpdump → 看重传
  │
  ├─ TIME-WAIT 堆积              → 短连接过多
  │    └─ sysctl 调 tcp_tw_reuse / tcp_max_tw_buckets
  │
  ├─ CLOSE-WAIT 堆积              → 应用忘记 close
  │    └─ lsof / strace 定位漏 close 的进程
  │
  └─ SYN queue 满               → SYN flood 或 backlog 太小
       └─ ss -lnt 看 Recv-Q
```

### 命令流程

```bash
# 1. 基础联通性
ping -c 3 example.com
traceroute example.com
mtr example.com         # 持续版 traceroute（更准）

# 2. 端口连通性
nc -vz example.com 443
# 输出：Connection to example.com 443 port [tcp/https] succeeded!

# 3. 本地监听状态
ss -tlnp | grep :8080
# LISTEN  0  128  *:8080  *:*  users:(("app",pid=1234,fd=6))

# 4. 连接状态分布
ss -tan | awk 'NR>1 {print $1}' | sort | uniq -c | sort -rn
# 输出：
#   15000 TIME-WAIT     ← ⚠️
#     500 ESTAB
#      10 LISTEN
#      42 CLOSE-WAIT    ← ⚠️

# 5. 某进程的所有连接
ss -tanp | grep pid=1234
lsof -p 1234 -i        # 同上，另一种视角

# 6. 抓包
sudo tcpdump -i eth0 -nn 'host 10.0.0.5 and port 80' -w cap.pcap
# 抓完用 wireshark 分析

# 7. 只看握手和 RST
sudo tcpdump -i eth0 -nn 'host 10.0.0.5' 'tcp[tcpflags] & (tcp-syn|tcp-rst) != 0'

# 8. 网卡错误
ip -s link show eth0
# TX/RX errors 不为 0 → 网卡或对端有问题

# 9. 内核参数
sysctl net.ipv4.tcp_tw_reuse
sysctl net.core.somaxconn      # backlog 上限
sysctl net.ipv4.tcp_max_syn_backlog
```

### 常见根因与修复

| 根因 | 关键信号 | 修复 |
|-----|---------|------|
| 短连接过多 | TIME-WAIT 万级 | 用连接池 / keepalive；`tcp_tw_reuse=1` |
| 应用漏 close | CLOSE-WAIT 堆积 | 代码修 defer close / body close |
| SYN queue 满 | Recv-Q 满 + 大量 SYN-RECV | 加大 somaxconn / tcp_max_syn_backlog |
| 端口耗尽（客户端） | connect: cannot assign requested address | 加大 `ip_local_port_range` |
| MTU 不匹配 | 大包能 ping 通、小包不通 | 调 MTU（`ip link set eth0 mtu 1400`） |
| DNS 慢 | curl 详细看 DNS 时间高 | 用本地缓存 nscd / systemd-resolved |
| SSL 握手慢 | TLS 时间占大头 | 复用 session / 用 keepalive |
| Nginx worker 过少 | 大量 SYN 排队 | 加 worker_processes / worker_connections |

### 真实案例：TIME-WAIT 打满

```bash
# 现象：应用 connect 报 "cannot assign requested address"

# 1. 看连接状态
$ ss -tan | awk 'NR>1 {print $1}' | sort | uniq -c | sort -rn
   28345 TIME-WAIT     ← 快撑爆端口
      42 ESTAB

# 2. 看端口范围
$ sysctl net.ipv4.ip_local_port_range
net.ipv4.ip_local_port_range = 32768   60999
# 只有 28231 个端口可用，已用 28345 → 用完了

# 3. 定位是谁
$ ss -tan state time-wait | awk '{print $4}' | awk -F: '{print $1}' | sort | uniq -c
   28000 10.0.0.5   ← 都连到同一个后端服务，短连接
     345 10.0.0.6

# 4. 修复方案组合拳
# a) 应用层：改用连接池 / keepalive（治本）
# b) 系统层：加大端口范围
sudo sysctl -w net.ipv4.ip_local_port_range="1024 65535"

# c) 允许复用 TIME-WAIT 连接（客户端安全）
sudo sysctl -w net.ipv4.tcp_tw_reuse=1

# d) 缩短 TIME-WAIT 保留时间（60s 是默认值，可调小但要谨慎）
# 注：net.ipv4.tcp_fin_timeout 只影响 FIN-WAIT-2，不是 TIME-WAIT

# 持久化到 /etc/sysctl.conf 后 sysctl -p 生效
```

---

## 六、应用无响应 / 卡死

### 现象特征

- 服务健康检查失败
- HTTP 请求超时（不返回 5xx，就是 hang）
- 日志停止输出
- 进程还活着（`ps` 有），但没在干活

### 决策树

```
应用不响应
  │
  ├─ 进程存在 + CPU 高          → 死循环 / 热点函数卡死
  │    └─ perf top / pprof CPU
  │
  ├─ 进程存在 + CPU 极低         → 全部阻塞
  │    ├─ pprof goroutine       → 全部在等 channel/锁
  │    ├─ jstack (Java)          → 全部 WAITING/BLOCKED
  │    └─ strace -p              → 卡在哪个 syscall
  │
  ├─ 进程存在 + 有 D 状态          → 磁盘卡死
  │    └─ cat /proc/PID/stack
  │
  ├─ 进程消失                    → 崩溃或被杀
  │    ├─ dmesg → OOM?
  │    ├─ core dump / 日志
  │    └─ systemctl status
  │
  └─ 端口没监听                  → 应用未启动或崩了
       └─ ss -tlnp | grep :端口
```

### 命令流程

```bash
# 1. 确认进程还在
ps aux | grep app-name
pgrep -f app-name

# 2. 端口在监听吗
ss -tlnp | grep :8080

# 3. CPU 高不高
top -p $(pgrep -d, -f app-name)
#      ↑ 只看这些进程

# 4. 卡在哪个 syscall
sudo strace -p 1234
# 常见卡死点：
#   futex(...)                → 等锁
#   epoll_wait(...)           → 等 IO（可能正常）
#   read(...)                 → 等 socket/pipe 数据
#   sched_yield(...)          → 自旋等待

# 5. Go 服务：pprof goroutine
curl 'localhost:6060/debug/pprof/goroutine?debug=2' > g.txt
# 看堆栈类型分布
grep "^goroutine" g.txt | awk '{print $NF}' | sort | uniq -c
#   [chan receive]: 10000    ← 都在等 chan，找 producer
#   [semacquire]: 500        ← 都在等 mutex

# 6. Java：thread dump
jstack -l 1234 > thread.dump
# 或（无侵入）
kill -3 1234    # 触发 JVM dump 到 stdout

# 7. 看有没有 fd 泄漏
sudo ls /proc/1234/fd | wc -l
# 越接近 ulimit 越危险

ulimit -n       # 当前进程 fd 上限
```

### 常见根因与修复

| 根因 | 关键信号 | 修复 |
|-----|---------|------|
| 死锁 | pprof 显示所有 g 都在等 mutex | 分析锁顺序、加超时 |
| 死循环 | CPU 100% + strace 无 syscall | pprof CPU 找热点 |
| 数据库连接池耗尽 | 都卡在 sql.DB.Query | 加大池 / 查慢 SQL |
| 下游依赖挂了 | 都卡在 net.Read | 加超时 / 熔断 |
| goroutine 泄漏 | numGoroutine 十万级 | 看 pprof goroutine 堆栈 |
| fd 耗尽 | too many open files | 加 ulimit / 修漏 close |
| 磁盘挂了 | D 状态 + iowait 100% | 修磁盘 / 迁移 |

### 真实案例：Go 服务卡死

```bash
# 现象：健康检查 30s 超时，restart 后又发生

# 1. 端口在监听
$ ss -tlnp | grep :8080
LISTEN 0 128 :::8080 :::*  ("app",pid=1234)

# 2. CPU 极低
$ top -p 1234
%CPU=0.3  %MEM=15  STATE=S

# 3. goroutine 数
$ curl -s localhost:6060/debug/pprof/goroutine?debug=1 | head -1
goroutine profile: total 18234    ← 1.8 万，异常

# 4. 看堆栈类型
$ curl -s 'localhost:6060/debug/pprof/goroutine?debug=2' \
    | grep -A 1 "^goroutine" | grep "\[" | sort | uniq -c | sort -rn | head
   17800 [chan send, 30 minutes]
     100 [IO wait]
     ...

# 5. 找 chan send 的堆栈
$ curl -s 'localhost:6060/debug/pprof/goroutine?debug=2' \
    | awk '/chan send, 30 minutes/,/^$/' | head -20
goroutine 45678 [chan send, 30 minutes]:
main.processTask
    /app/task.go:42

# 6. 定位：task.go:42 是把结果发到 result chan
#         上游读 chan 的逻辑在超时后直接 return，没消费
#         结果 chan 无缓冲，全部 send goroutine 卡死

# 修复：用带缓冲 channel 或用 select + default 非阻塞
select {
case resultCh <- result:
case <-ctx.Done():
    return
}
```

---

## 七、USE 方法与 60 秒诊断

### USE 方法（Brendan Gregg）

对每个资源检查三件事：

- **U**tilization：使用率（工作时间占比）
- **S**aturation：饱和度（等待队列长度）
- **E**rrors：错误数

| 资源 | Utilization | Saturation | Errors |
|-----|-------------|-----------|--------|
| CPU | `%usr+%sys` | load / run queue | (无常规指标) |
| 内存 | used / total | swap 使用、si/so | OOM kills |
| 磁盘 | %util | avgqu-sz / await | dmesg IO error |
| 网卡 | Kbps / 带宽 | 队列丢包 | RX/TX errors |

### 60 秒诊断法

Netflix 生产标准，遇到问题先跑这 10 条：

```bash
uptime                     # 1. Load 概览
dmesg | tail               # 2. 内核最近事件（OOM、崩溃）
vmstat 1                   # 3. CPU/内存/IO 全景 + 上下文切换
mpstat -P ALL 1            # 4. 每核 CPU
pidstat 1                  # 5. 每进程 CPU
iostat -xz 1               # 6. 磁盘 IO
free -m                    # 7. 内存
sar -n DEV 1               # 8. 网卡吞吐
sar -n TCP,ETCP 1          # 9. TCP 连接与重传
top                        # 10. 交互式看
```

**60 秒内**基本能判断"是哪一类问题"，再针对性深挖。

### 排查清单模板

遇到线上问题时按此顺序问自己：

```
□ 什么时候开始的？（对齐发布 / 流量 / cron）
□ 影响面：单实例 vs 全集群？
□ 有没有 dmesg 硬件事件？
□ 请求增多了吗？（Traffic 变了？）
□ 依赖服务健康吗？（下游拉挂了？）
□ 内存/CPU/磁盘/网络哪个先饱和？
□ 有没有 core dump / crash log？
□ 能不能快速回滚？（先止血再定位）
```

---

## 八、下一步

- **面试题演练**：见 [interview-questions.md](./interview-questions.md)
- **工具速查**：见 [perf-tools.md](./perf-tools.md)
- **命令示例**：见 [commands.md](./commands.md)
