# Linux 性能排查工具箱

**标签**: #linux #performance #perf #strace #flamegraph #高频

按 **CPU / 内存 / 磁盘 / 网络 / 综合 / 进程追踪 / 火焰图** 七大类整理。配合 [troubleshooting.md](./troubleshooting.md) 的排查流程使用。

---

## 目录

1. [CPU 类](#一cpu-类)
2. [内存类](#二内存类)
3. [磁盘类](#三磁盘类)
4. [网络类](#四网络类)
5. [综合监控类](#五综合监控类)
6. [进程追踪类](#六进程追踪类)
7. [火焰图](#七火焰图)
8. [速查对照表](#八速查对照表)

---

## 一、CPU 类

### 1) mpstat — 每核 CPU 使用率

`mpstat` 来自 `sysstat` 包，专门看**每个 CPU 核心**的使用率（top 汇总，mpstat 细分）。

```bash
# 每 1 秒采样，采 5 次，看所有核心
mpstat -P ALL 1 5
#        ↑ 所有核心（不加就是汇总）
#             ↑ 间隔    ↑ 次数
```

**输出示例**：

```
10:30:15  CPU  %usr  %nice  %sys %iowait  %irq  %soft %steal %guest  %idle
10:30:16  all  15.20  0.00  3.50   0.30   0.00   0.20  0.00   0.00   80.80
10:30:16    0  20.10  0.00  4.20   0.50   0.00   0.30  0.00   0.00   74.90
10:30:16    1  12.30  0.00  3.10   0.20   0.00   0.10  0.00   0.00   84.30
```

**字段解读**：

| 字段 | 含义 | 关注点 |
|-----|------|-------|
| `%usr` | 用户态 CPU | 高说明应用计算量大 |
| `%sys` | 内核态 CPU | 高说明系统调用/上下文切换多 |
| `%iowait` | 等 IO 空闲 | ⚠️ 高说明磁盘/网络瓶颈 |
| `%irq` | 硬中断 | 高说明中断风暴（网卡/磁盘） |
| `%soft` | 软中断 | 高说明网络包处理繁忙（netfilter/tc） |
| `%steal` | 被虚拟化偷走 | ⚠️ 云主机上高说明宿主机争抢 |
| `%idle` | 空闲 | 100 - 其他值之和 |

**判读心法**：
- `%usr + %sys` 高 → 应用 CPU 密集，看 pprof/perf
- `%iowait` 高 → 转去看磁盘（iostat）
- `%soft` 高 → 转去看网络（tcpdump）
- **单核 100% 而其他核空闲** → 应用没利用多核（Go 检查 GOMAXPROCS）

### 2) pidstat — 按进程看资源使用

`sysstat` 包里另一大杀器，比 top 更精细：

```bash
# 每秒采样，每个进程的 CPU 使用（按进程平均）
pidstat 1
#        ↑ 间隔（不指定次数 = 一直采）

# 按线程展开（一个进程的每个线程独立）
pidstat -t 1
#        ↑ -t 显示线程

# 只看内存
pidstat -r 1
#        ↑ -r 内存

# 只看 IO
pidstat -d 1
#        ↑ -d 磁盘

# 只看某个进程
pidstat -p 1234 1

# 上下文切换（面试考点：判断锁竞争）
pidstat -w 1
#        ↑ -w 上下文切换
```

**输出示例（-w 上下文切换）**：

```
10:30:15   UID  PID   cswch/s  nvcswch/s  Command
10:30:16  1000 1234    250.00     1500.00  java
                       ↑          ↑
                     主动          被动
```

- **主动切换 (cswch)**：进程主动让出（等 IO、等锁）
- **被动切换 (nvcswch)**：时间片用完被抢

**被动切换特别高**（几万/秒） → CPU 竞争或 GOMAXPROCS 过大。
**主动切换特别高** → 锁竞争或频繁 IO。

### 3) perf — 内核级性能分析

`perf` 是 Linux 内核自带的性能分析工具，最强大也最难。

```bash
# 采样 30 秒，看全系统热点
sudo perf top

# 记录某个命令的执行
sudo perf record -F 99 -a -g -- sleep 30
#                 ↑ 频率 99Hz  ↑ 全系统   ↑ 采样调用栈    ↑ 采样时长
sudo perf report                # 交互式查看

# 只看某个进程
sudo perf record -F 99 -p 1234 -g -- sleep 30
sudo perf report

# 统计模式（不采样，直接算总数）
sudo perf stat -p 1234 -- sleep 10
# 输出：
#      CPU utilized      : 2.1
#      context-switches  : 15,234
#      cpu-migrations    : 42
#      page-faults       : 1,234
#      cycles            : 8.5B
#      instructions      : 12B     ← 每周期指令数 (IPC) = 12B / 8.5B = 1.41
```

**perf 事件类型**：

```bash
sudo perf list          # 看所有可采集事件

# 典型：
#   cpu-cycles          CPU 周期
#   instructions        指令数
#   cache-misses        缓存未命中（内存瓶颈的关键信号）
#   branch-misses       分支预测失败
```

**perf 生成火焰图**：见第七节。

### 4) top / htop（在 commands.md 已讲）

**性能排查视角的额外读法**：

```
%Cpu(s):  5.2 us,  1.3 sy,  0.0 ni, 93.0 id,  0.5 wa,  0.0 hi,  0.0 si,  0.0 st
          ↑ us    ↑ sy     ↑ nice  ↑ 空闲  ↑ iowait ↑ 硬中断 ↑ 软中断 ↑ steal
```

跟 mpstat 一样的字段，重点判读 `us / sy / wa / si / st`。

---

## 二、内存类

### 1) free — 内存概览

```bash
free -h
#     ↑ 人类可读单位（KB/MB/GB）
```

**输出示例**：

```
              total        used        free      shared  buff/cache   available
Mem:           16Gi        4Gi         2Gi        100Mi        10Gi        11Gi
Swap:          4Gi         0B          4Gi
```

**字段解读（最容易搞错的地方）**：

- **total**：物理内存总量
- **used**：真正被应用占用（**不含 buff/cache**）
- **free**：完全空闲（数字通常很小，不用担心）
- **buff/cache**：内核用来加速 IO 的缓冲区，**随时可回收**
- **available** ★：**真正可用**（free + 可回收的 cache）

**❌ 面试陷阱**：看到 free 很低就说"内存不够了"是错的——Linux 会主动用满 buff/cache 加速 IO，`available` 才是关键。

**判断内存压力**：
- `available` < total 的 20% → 开始警惕
- `Swap used` > 0 且持续增长 → **真的不够，OOM 快了**

### 2) vmstat — 虚拟内存 + 全局状态

```bash
vmstat 1 5
#       ↑ 间隔  ↑ 次数
```

**输出**：

```
procs -----------memory---------- ---swap-- -----io---- -system-- ------cpu-----
 r  b   swpd   free   buff  cache   si   so    bi    bo   in   cs us sy id wa st
 2  0      0 2097152 102400 10485760  0    0    45    89  520 1200 15  3 82  0  0
```

**字段解读**：

| 字段 | 含义 | 关注 |
|-----|------|------|
| `r` | 运行队列长度 | > CPU 核数说明有等待 |
| `b` | 阻塞进程数 | > 0 且持续 → 有 IO 或锁瓶颈 |
| `swpd` | 已用 swap | > 0 → 内存吃紧 |
| `si` | 从 swap 换入 KB/s | > 0 → 内存严重不足 |
| `so` | 换出到 swap KB/s | 同上 |
| `bi` | 读磁盘 blocks/s | 高 → 读 IO 大 |
| `bo` | 写磁盘 blocks/s | 高 → 写 IO 大 |
| `in` | 中断次数/s | 突增 → 硬件事件 |
| `cs` | 上下文切换/s | 突增 → 锁竞争 |
| `us/sy/id/wa/st` | 同 mpstat |  |

**si/so 有值时基本可以判定"内存瓶颈"**——swap 是灾难信号。

### 3) pmap — 某进程的内存映射

```bash
pmap -x 1234
#     ↑ -x 详细模式（Kbytes / RSS / Dirty）
```

**输出示例**：

```
Address           Kbytes     RSS   Dirty Mode  Mapping
0000000000400000    3396    2100       0 r-x-- java
00007f8c00000000  524288  102400  102400 rw---   [ anon ]
                                              ↑ 匿名映射（堆/栈）
                  ...
                --------  -------  ------
total          1200000    600000  500000
```

**字段解读**：
- `Kbytes`：虚拟映射大小
- `RSS`：实际驻留物理内存（重要）
- `Dirty`：脏页（未写回磁盘）
- `Mode`：`r`=可读，`w`=可写，`x`=可执行

**使用场景**：看某进程的内存都被哪些映射占了，能否定位大内存来源（堆 vs mmap 文件 vs .so 库）。

### 4) /proc/meminfo — 最详细的内存信息

```bash
cat /proc/meminfo | head -20
```

关键字段：

```
MemTotal:       16000000 kB
MemFree:         2000000 kB
MemAvailable:   11000000 kB     ← 同 free -h 的 available
Buffers:          100000 kB
Cached:         10000000 kB
SwapCached:            0 kB
Active:          8000000 kB     ← 最近用过的
Inactive:        2000000 kB     ← 长时间没用
Slab:             500000 kB     ← 内核对象缓存
Committed_AS:   20000000 kB     ← 已承诺的总内存（可能超物理内存）
```

---

## 三、磁盘类

### 1) iostat — IO 统计

```bash
iostat -x 1
#       ↑ 扩展模式（更多字段）  ↑ 每秒采样
```

**输出**：

```
Device  r/s  w/s  rkB/s  wkB/s  rrqm/s wrqm/s  ...  await r_await w_await  ... %util
sda    30.5 10.2  512    256    0.5    2.1          8.5   6.2     10.8         85.20
                                                                                  ↑ 关键
```

**核心字段**：

| 字段 | 含义 | 关注 |
|-----|------|------|
| `r/s` `w/s` | 每秒读/写次数（IOPS） | HDD 极限 ~200，SSD 极限 10万+ |
| `rkB/s` `wkB/s` | 每秒读/写 KB | 顺序 IO 看这个 |
| `rrqm/s` `wrqm/s` | 每秒合并的请求数 | 合并率高说明 IO 顺序性好 |
| `r_await` `w_await` | 读/写平均等待时间 (ms) | HDD < 10ms、SSD < 1ms 正常 |
| `avgqu-sz` | 平均队列长度 | > 1 说明有排队 |
| **`%util`** ★ | 磁盘忙碌百分比 | > 80% 说明磁盘瓶颈 |

**判读**：`%util = 100` 且 `await` 高 → 磁盘就是瓶颈。

### 2) iotop — 按进程看 IO

```bash
sudo iotop -oP
#           ↑ -o 只显示有 IO 的进程
#             ↑ -P 显示进程（不加是线程）
```

**输出**：

```
Total DISK READ:    5.2 M/s | Total DISK WRITE:    12.4 M/s
  PID  PRIO  USER    DISK READ  DISK WRITE  SWAPIN     IO>    COMMAND
 1234  be/4  mysql    2.1 M/s     8.5 M/s   0.00%   45.2 %    mysqld
 5678  be/4  redis    0.5 M/s     0.2 M/s   0.00%    2.1 %    redis-server
```

**IO>** 列是核心：**在等 IO 上花的时间占比**——90% 说明这个进程主要在等磁盘。

### 3) du — 目录空间占用

```bash
# 当前目录及子目录汇总
du -sh .

# 当前目录下每个直接子项
du -sh */

# 找最大的 10 个子目录
du -sh /var/* 2>/dev/null | sort -rh | head -10
#                                ↑ -h 保留人类可读单位排序

# 深度控制
du -h --max-depth=2 /var
```

**注意**：`du` 慢，因为要遍历所有文件。大目录用 `df` 看整体再定位。

### 4) df — 文件系统整体使用

```bash
df -h
#   ↑ 人类可读
```

**输出**：

```
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1       100G   80G   20G  80% /
/dev/sdb1       500G  100G  400G  20% /data
tmpfs           8.0G     0  8.0G   0% /dev/shm
```

- **Use%** > 90% → 快满了，赶紧清理
- **Use%** 到 100% → **数据库/日志会写失败**

**inode 也可能满**（文件数太多，即使磁盘空间还有）：

```bash
df -i
```

看 `IUse%`，如果 100% 但空间还有 → 有大量小文件占满 inode，删除即可。

---

## 四、网络类

### 1) iftop — 实时网络流量（按连接）

```bash
sudo iftop -i eth0
#          ↑ 指定网卡
```

**输出**：

```
                12.5Kb        25.0Kb        37.5Kb          50.0Kb        62.5Kb
├──────────────┼──────────────┼──────────────┼──────────────┼──────────────┤
10.0.0.5       => 10.0.0.100   4.5Mb  3.2Mb  2.1Mb
               <=              1.2Mb  850Kb  600Kb
                              ↑ 当前   ↑ 10s   ↑ 40s 平均
```

**用途**：找出流量最大的**IP 对**，判断是被谁打爆了。

### 2) nethogs — 实时流量（按进程）

```bash
sudo nethogs eth0
```

**输出**：

```
Refreshing:
PID  USER    PROGRAM          DEV  SENT   RECEIVED
1234 www     nginx: worker    eth0 1.5    0.8       KB/sec
5678 mysql   mysqld           eth0 0.5    3.2       KB/sec
```

**用途**：找出**哪个进程**在耗流量。iftop 按连接看，nethogs 按进程看，互补。

### 3) ss / tcpdump

见 [commands.md](./commands.md) 第五节。性能排查视角关注：

```bash
# 统计连接状态分布（判断有没有 TIME-WAIT / CLOSE-WAIT 堆积）
ss -tan | awk 'NR>1 {print $1}' | sort | uniq -c
# 输出示例：
#     42 ESTAB
#  15234 TIME-WAIT     ← ⚠️ 极大堆积说明短连接过多
#    892 CLOSE-WAIT    ← ⚠️ 应用漏 close

# 抓某端口的 TCP 头看重传/RST
sudo tcpdump -i eth0 -nn port 8080 | grep -E "Flags \[R" 
#                                          ↑ 看 RST（连接被拒/超时）
```

---

## 五、综合监控类

### 1) dstat — 多维度综合

`dstat` 一屏看 CPU/内存/磁盘/网络/进程：

```bash
dstat -tcmnd 1
#      ↑ 时间戳 CPU 内存 网络 磁盘   ↑ 间隔
```

**输出**：

```
----system---- --total-cpu-usage-- ------memory------- -net/total- -dsk/total-
   time        usr sys idl wai hiq siq| used  buff  cach|  recv  send|  read  writ
22-09 10:30:15  15   3  82   0   0   0|4.0G  100M  10G|  1.2M  850K|  512k  256k
```

一目了然，比逐个工具跑省事。

### 2) sar — 历史数据回溯

`sar` 是 `sysstat` 包里最强的**历史归档**工具，默认每 10 分钟采一次系统状态存到 `/var/log/sysstat/`。

```bash
# 看今天的 CPU
sar

# 看指定日期（05 号）
sar -f /var/log/sysstat/sa05

# 只看内存
sar -r

# 只看磁盘
sar -d

# 只看网络
sar -n DEV

# 只看某个时段
sar -s 10:00:00 -e 12:00:00
```

**核心价值**：**故障后回看**——`top` 只能看实时，`sar` 能查一周前 3:15 分那个时点的负载。**运维必装**。

启用：

```bash
apt install sysstat        # Debian/Ubuntu
yum install sysstat        # RHEL/CentOS
systemctl enable sysstat && systemctl start sysstat
# 修改 /etc/default/sysstat 设 ENABLED="true"
```

---

## 六、进程追踪类

### 1) strace — 系统调用追踪

**看进程调了哪些 syscall**，无需改代码，超强黑盒排查工具。

```bash
# 跟踪某个已运行的进程
sudo strace -p 1234

# 启动时跟踪
strace ./program

# 只看某类调用
strace -e trace=open,read,write ./program
#      ↑ 只看文件相关

strace -e trace=network ./program
#                     ↑ 网络相关（socket/connect/send/recv）

# 统计每个 syscall 的耗时和次数
strace -c -p 1234
# 按 Ctrl+C 结束，输出：
# % time  seconds  usecs/call  calls  errors  syscall
#  45.2   0.08     8           10000  0       read
#  30.1   0.05     50          1000   0       write
#  ...

# 跟踪并追加时间戳
strace -tt -T -p 1234
#       ↑ 微秒时间戳
#         ↑ 每个 syscall 耗时
```

**输出示例**：

```
open("/etc/hosts", O_RDONLY)              = 3
read(3, "127.0.0.1 localhost\n"..., 4096) = 89
close(3)                                   = 0
```

**典型排查**：
- 应用卡死 → `strace -p PID` 看它在等哪个 syscall
- 慢启动 → 看是不是死在 DNS/证书/文件加载
- 权限报错 → 看是哪个文件的 open 返回 EACCES

**⚠️ 注意**：strace 会显著拖慢被跟踪进程（可能 10x 慢），生产用要谨慎。

### 2) ltrace — 库函数追踪

跟 strace 类似，但追踪的是**动态库函数调用**（如 malloc、printf）：

```bash
ltrace ./program
ltrace -p 1234
```

用得比 strace 少，主要看内存分配（malloc/free）泄漏。

---

## 七、火焰图

`perf` + [FlameGraph](https://github.com/brendangregg/FlameGraph)（Brendan Gregg 的脚本），生成可视化的 CPU 火焰图。

### 完整流程

```bash
# 1. 装 FlameGraph 脚本
git clone https://github.com/brendangregg/FlameGraph.git

# 2. 用 perf 采样某进程 30 秒
sudo perf record -F 99 -p 1234 -g -- sleep 30
#                 ↑ 99Hz 频率  ↑ 进程 PID  ↑ 采调用栈

# 3. 导出为 perf 脚本格式
sudo perf script > out.perf

# 4. 折叠调用栈（相同栈合并计数）
./FlameGraph/stackcollapse-perf.pl out.perf > out.folded

# 5. 生成 SVG
./FlameGraph/flamegraph.pl out.folded > flame.svg

# 6. 浏览器打开 flame.svg
```

### 火焰图判读

```
横轴：CPU 占比（越宽越耗）
纵轴：调用栈深度（顶部是 CPU 上正在跑的函数）
颜色：无意义（区分用）

看图诀窍：
1. 找 "又宽又平的顶部" → 就是热点
2. 从顶部往下追，看是谁调用的
```

**典型模式**：

- 一大坨 `runtime.mallocgc` / `sys_futex` / `kmem_cache_alloc` 在顶部 → 分配/锁问题
- `[kernel]` 部分很宽 → 系统调用/中断处理占比高
- 应用函数占最多 → 计算密集，看能否算法优化

### Go 应用直接用 pprof 更快

Go 应用不需要 perf，`go tool pprof -http=:8080 cpu.prof` 自带火焰图（详见 [pprof-guide.md](../../go/performance/pprof-guide.md)）。

**perf + FlameGraph 的优势**：
- 能采**内核栈**（syscall/中断细节）
- 能采**其他语言进程**（Python/C++/Java）
- 能看**跨进程调度**

---

## 八、速查对照表

**遇到什么问题用什么工具**：

| 现象 | 首选工具 | 深入工具 |
|-----|---------|---------|
| CPU 100% | `top` → 找进程 | `pidstat -t` → 找线程；`perf top` → 找函数 |
| 内存高 / OOM | `free -h` → 判断压力 | `pmap` → 找热点；`/proc/PID/status` |
| 磁盘 IO 满 | `iostat -x 1` → 看 %util | `iotop -oP` → 找进程；`strace -e file` |
| 网络异常 | `ss -s` → 连接总览 | `ss -tan` → 状态分布；`tcpdump` → 抓包；`iftop` → 流量 |
| 应用卡死 | `strace -p PID` → 看 syscall | `perf record -g` → 看调用栈 |
| 上下文切换高 | `vmstat 1` → cs 列 | `pidstat -w 1` → 定位进程 |
| 历史故障回看 | `sar -f /var/log/...` | 结合 grafana/prometheus 长期数据 |

**关键参数默认值**：

| 工具 | 默认间隔 | 推荐间隔 |
|-----|---------|---------|
| mpstat / iostat / vmstat / pidstat | 一次性 | `1 5`（1 秒采 5 次） |
| dstat | 1 秒 | 保持 |
| perf record | — | `-F 99` |
| sar | 10 分钟 | 保持 |

---

## 九、快速安装

```bash
# Debian/Ubuntu
sudo apt install -y sysstat iotop iftop nethogs htop dstat strace ltrace linux-tools-generic

# RHEL/CentOS
sudo yum install -y sysstat iotop iftop nethogs htop dstat strace ltrace perf

# 开启 sar 历史数据
sudo systemctl enable --now sysstat
```

---

**下一步**：
- 系统性排查流程见 [troubleshooting.md](./troubleshooting.md)
- 面试常问见 [interview-questions.md](./interview-questions.md)
