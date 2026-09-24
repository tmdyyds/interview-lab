# Linux 面试高频题（含完整答案）

**标签**: #linux #interview #ops #高频

覆盖 README 点名的 4 道核心题目 + 20 道扩展题。每题都给**完整命令 + 逐段拆解 + 常见追问**。

---

## 目录

- [Part 1：核心必背 4 题](#part-1核心必背-4-题)
- [Part 2：命令行手感](#part-2命令行手感)
- [Part 3：性能与排查](#part-3性能与排查)
- [Part 4：权限与账号](#part-4权限与账号)
- [Part 5：进程与信号](#part-5进程与信号)
- [Part 6：文件系统与磁盘](#part-6文件系统与磁盘)
- [Part 7：网络](#part-7网络)

---

## Part 1：核心必背 4 题

### Q1: 如何排查 CPU 100%？

**四步定位法**：

```bash
# 第一步：宏观看是哪种 CPU 时间
$ mpstat -P ALL 1 3
# 关注 %usr（用户态）/ %sys（内核态）/ %iowait（等 IO）/ %soft（软中断）

# 第二步：找到 CPU 高的进程
$ top -o %CPU        # 或 pidstat 1 3
#      ↑ 按 CPU 排序

# 第三步：找到进程内 CPU 高的线程
$ top -H -p 1234
#      ↑ -H 显示线程       ↑ 只看这个进程

# 输出：
# PID  PR NI VIRT RES SHR S %CPU %MEM  TIME+ COMMAND
# 5678 20  0  10G 5G  10M R  95.0 30.0 5:23  java-worker
#                            ↑ 这个 TID 消耗 95%

# 第四步：看这个线程栈
# a) Java：
$ printf '%x\n' 5678               # 十进制 TID 转 16 进制
1652
$ jstack 1234 | grep -A 30 nid=0x1652
#                                 ↑ jstack 里 nid 是 16 进制线程 ID

# b) Go：
$ curl 'http://localhost:6060/debug/pprof/profile?seconds=30' -o cpu.prof
$ go tool pprof -top cpu.prof

# c) C/C++/通用：
$ sudo perf top -p 1234 -g
```

**追问 1：mpstat 里 iowait 高怎么办？**

`%iowait` 高说明 CPU 在等磁盘/网络，**不是 CPU 忙**。

- 转去看 `iostat -xz 1` 的 `%util` 和 `await`
- 用 `iotop -oP` 找 IO 大户
- 看 D 状态进程：`ps aux | awk '$8~/D/'`

**追问 2：为什么单核 100% 而其他核空闲？**

- 应用是单线程 / 没利用多核
- Python GIL（Python 除非用 multiprocessing，否则 CPU 密集只跑一核）
- Node.js 单线程（用 cluster / worker_threads 才能多核）
- Go 服务但 `GOMAXPROCS=1`

**追问 3：cgroup 限制的容器里 CPU 100% 是什么意思？**

看**容器的 CPU quota**：

```bash
cat /sys/fs/cgroup/cpu/cpu.cfs_quota_us    # 分子（单位微秒）
cat /sys/fs/cgroup/cpu/cpu.cfs_period_us   # 分母
# quota=200000, period=100000 → 允许用 2 核 CPU
```

容器内 top 看到 100% 可能是"耗完了限额"，宿主机可能还很闲。

---

### Q2: 如何排查内存泄漏？

**步骤**：

```bash
# 第一步：看是不是真的内存不够
$ free -h
              total   used   free   available
Mem:          16Gi    14Gi   500Mi  1Gi        ← available 快没了
Swap:         4Gi     3.5Gi  500Mi                ← swap 也用了

# 第二步：找内存大户
$ ps -eo pid,user,rss,vsz,comm --sort=-rss | head -10
#           ↑ RSS 是实际物理内存
   PID  USER     RSS   VSZ  COMMAND
  1234  jake  5000000 8000000  java-app       ← 就是它，5G

# 第三步：确认是否持续增长（关键！单次 RSS 大不一定是泄漏）
$ while true; do
    date; ps -p 1234 -o rss=; sleep 60;
  done
# Sep 22 10:00  4900000
# Sep 22 10:01  5000000
# Sep 22 10:02  5100000            ← 每分钟涨 100MB → 泄漏

# 第四步：定位泄漏源
# Go 服务：
$ curl 'http://localhost:6060/debug/pprof/heap' -o heap.out
$ go tool pprof -top heap.out

# Java 服务：
$ jmap -histo:live 1234 | head -20
# 或 dump 出来用 MAT 分析
$ jmap -dump:live,format=b,file=heap.hprof 1234

# C/C++/通用：
$ valgrind --leak-check=full ./program        # 需要重启带 valgrind
```

**追问 1：`RSS` 和 `VSZ` 有什么区别？**

- **VSZ**（Virtual Size）：**申请的**虚拟内存，含 mmap 但未用的
- **RSS**（Resident Set Size）：**实际驻留物理内存**

进程分配了 10G 虚拟内存但只用了 100M，那 VSZ=10G、RSS=100M。**看内存问题主要看 RSS**。

**追问 2：free 显示 buff/cache 很高，是不是泄漏？**

不是。buff/cache 是 **内核用的文件缓存**，随时可以被回收给应用，看 `available` 才是真实空余。

强制清 cache 演示（生产别乱用）：

```bash
sync                                # 先把脏页写盘
echo 3 > /proc/sys/vm/drop_caches   # 3 = 清页缓存 + slab
```

**追问 3：明明 available 还有很多，为什么应用还是 OOM？**

- **容器场景**：cgroup memory limit 触发，宿主机还闲
- **JVM 场景**：JVM 堆达到 `-Xmx` 上限，宿主机内存不管
- **Go 场景**：GOMEMLIMIT 触发 GC 频繁但内存持续增长
- **NUMA 场景**：某个 NUMA node 内存耗尽，`numactl` 排查

---

### Q3: 如何统计某端口的连接数？

**多个角度**：

```bash
# 方式 1：统计总数
$ ss -tan | grep ':8080' | wc -l
#   -t TCP  -a 全部  -n 数字（不 DNS）
#                                 ↑ 行数就是连接数

# 方式 2：按连接状态分组
$ ss -tan | grep ':8080' | awk '{print $1}' | sort | uniq -c | sort -rn
     42 ESTAB
     18 TIME-WAIT
      3 LISTEN

# 方式 3：按对端 IP 分组（找谁连得最多）
$ ss -tan | grep ':8080' | grep ESTAB | awk '{print $5}' | awk -F: '{print $1}' \
    | sort | uniq -c | sort -rn | head -10
#        ↑ 第 5 列是对端 addr:port
#                                    ↑ 按 : 切，取 IP 部分
    120 10.0.0.5
     85 10.0.0.6

# 方式 4：某进程有多少连接
$ ss -tanp | grep ':8080' | grep pid=1234 | wc -l
$ lsof -i :8080 -n | wc -l           # 同上，另一种视角
$ ls /proc/1234/fd | wc -l           # 该进程打开的 fd 数（含非 socket）
```

**追问 1：`netstat` 和 `ss` 有什么区别？**

- `netstat` 老命令，遍历 `/proc/net/tcp`，几万连接就很慢
- `ss` 用 `netlink` 直接问内核，快 10-100 倍，且信息更全

现代系统都推荐 `ss`。

**追问 2：ESTABLISHED / TIME-WAIT / CLOSE-WAIT 有什么区别？**

```
建立：      SYN → SYN-ACK → ACK → ESTABLISHED
主动关闭：  FIN → FIN-WAIT-1 → FIN-WAIT-2 → TIME-WAIT (60s) → CLOSED
被动关闭：  收到 FIN → CLOSE-WAIT → 应用调 close → LAST-ACK → CLOSED
```

- **TIME-WAIT 堆积**：主动关闭方在等 2MSL，短连接场景常见，一般无害但可能耗端口
- **CLOSE-WAIT 堆积**：被动关闭方**应用忘了调 close**，是 **代码 bug**

**追问 3：一个端口能建立多少连接？**

不是端口限制，而是 **五元组唯一性限制**：

```
(源 IP, 源端口, 目的 IP, 目的端口, 协议)
```

- **服务端**：一个监听端口能接**几十万连接**（受 fd 上限、内存、backlog 影响）
- **客户端**：从同一个 IP 出发连**同一个后端 (IP+端口)**，最多 `65535 - 32768 = 32767` 个连接（`ip_local_port_range`）

---

### Q4: awk 一行统计 Nginx 日志各状态码数量

```bash
$ awk '{print $9}' access.log | sort | uniq -c | sort -rn
```

**逐段拆解**：

```bash
awk '{print $9}' access.log
# ↑ 打印第 9 列（Nginx combined 格式里状态码位置）
# 输出示例：
#   200
#   200
#   404
#   200
#   500

| sort
# ↑ 排序（uniq 只对相邻行去重，必须先 sort）

| uniq -c
# ↑ -c 计数
# 输出：
#   15234 200
#     421 404
#      18 500

| sort -rn
# ↑ -r 逆序 -n 按数字排
# 最终：
#   15234 200
#     421 404
#      18 500
```

**Nginx combined 日志格式对照**：

```
$remote_addr $remote_user [$time_local] "$request" $status $body_bytes_sent "$referer" "$user_agent"
     $1        $3          $4 $5          $6 $7 $8    $9        $10             $11        $12
```

所以 **状态码是第 9 列**。

**追问 1：只用 awk 一条命令搞定（不用管道）？**

```bash
awk '{codes[$9]++} END {for (c in codes) print codes[c], c}' access.log | sort -rn
```

- `codes[$9]++` — 用状态码做 key，出现一次 +1
- `END {}` — 文件读完执行
- `for (c in codes)` — 遍历 hash 表

**追问 2：怎么统计 5xx 错误率？**

```bash
awk 'BEGIN {total=0; err=0}
     {total++; if ($9 ~ /^5/) err++}
     END {printf "%.2f%%\n", err/total*100}' access.log
# 输出：0.35%
```

**追问 3：找出 5xx 请求的 top URL**

```bash
awk '$9 ~ /^5/ {print $7}' access.log | sort | uniq -c | sort -rn | head -10
#    ↑ 只处理 5xx        ↑ 第 7 列是请求 URI
```

**追问 4：按分钟看 5xx 数量**

```bash
awk '$9 ~ /^5/ {print substr($4, 2, 17)}' access.log | uniq -c
#                     ↑ [22/Sep/2026:10:30 取到分钟精度
# 输出：
#     3 22/Sep/2026:10:30
#    12 22/Sep/2026:10:31   ← 突增点
#     5 22/Sep/2026:10:32
```

---

## Part 2：命令行手感

### Q5: 单引号、双引号、反引号有什么区别？

```bash
name="jake"

echo '$name'          # 输出：$name           单引号：原样
echo "$name"          # 输出：jake            双引号：解析变量
echo `date`           # 输出：Mon Sep 22...   反引号：执行命令
echo $(date)          # 同上，推荐这个（更清晰、可嵌套）
```

**规则**：
- 单引号里的一切都是字面量
- 双引号解析 `$var` 和 ``` `cmd` ``` 、`$(cmd)`
- `$(...)` 优于反引号（可嵌套 `$(cat $(ls))`）

### Q6: `>` `>>` `2>&1` 分别是什么？

```bash
cmd > out.txt          # stdout 重定向（覆盖）
cmd >> out.txt         # stdout 追加
cmd 2> err.txt         # stderr 重定向
cmd > out.txt 2>&1     # stdout + stderr 都到 out.txt
cmd &> out.txt         # 简写（bash 特有）
cmd > /dev/null 2>&1   # 静默运行（丢弃所有输出）
```

**顺序有讲究**：`2>&1` 必须放在 `> file` **之后**，因为它是"把 stderr 指向 stdout **当前**的位置"。

```bash
cmd 2>&1 > file        # ❌ 错：stderr 指向了终端，不是 file
cmd > file 2>&1        # ✅ 对
```

### Q7: `|` 和 `xargs` 的区别？

```bash
# | 传"文本"
ls | cat
# cat 读的是标准输入的字符流

# xargs 传"参数"
ls | xargs rm
# xargs 把 stdin 的每行变成 rm 的参数：rm file1 file2 ...
```

**用 xargs 的场景**：目标命令**不接受 stdin，只接受参数**（如 `rm`、`cp`、`kill`）。

```bash
# 常用组合
find /tmp -name "*.tmp" | xargs rm
find . -name "*.log" | xargs grep "ERROR"

# 有空格的文件名要用 -0
find . -name "*.log" -print0 | xargs -0 rm
#                         ↑ 用 null 字符分隔  ↑ 对应处理
```

### Q8: 怎么用 tar 打包/解压？

```bash
# 打包
tar cvf archive.tar dir/       # 不压缩
tar czvf archive.tar.gz dir/   # gzip 压缩
tar cjvf archive.tar.bz2 dir/  # bzip2（更小更慢）
tar cJvf archive.tar.xz dir/   # xz（最小最慢）

# 解压（记住：j 换成对应压缩类型的字母）
tar xvf archive.tar
tar xzvf archive.tar.gz
tar xjvf archive.tar.bz2

# 参数记忆
# c create   打包
# x extract  解压
# t list     只看不解
# v verbose  显示过程
# f file     指定文件（必须在最后）
# z gzip
# j bzip2
# J xz
```

**追问：只想看压缩包里有什么？**

```bash
tar tzvf archive.tar.gz | head -20
```

---

## Part 3：性能与排查

### Q9: Load average 是什么？多少算高？

**定义**：单位时间内**平均处于 Running + Uninterruptible** 状态的进程数。

**关键点**：
- 不是"CPU 使用率"，而是"**等 CPU + 等磁盘的进程数**"
- 三个数字对应 1 / 5 / 15 分钟平均

**判读**：
- `load / CPU 核数 < 0.7` → 健康
- `1.0` → 满载
- `> 1.0` → 过载
- 8 核机器 load = 8 才等价于"单核 100%"

**追问：load 高但 CPU 空闲，怎么回事？**

大概率是**大量 D 状态进程**（等磁盘 IO）——它们也计入 load，但不占 CPU。

```bash
ps aux | awk '$8 ~ /D/'
cat /proc/PID/stack     # 看卡在哪个内核函数
```

### Q10: 什么是僵尸进程？怎么处理？

**僵尸**：子进程结束了，但父进程没调 `wait()` 收尸，进程表里残留一条记录（`Z` 状态）。

**危害**：占进程号（PID 有限），太多会导致新进程创建失败。

**处理**：

```bash
# 找到僵尸和它的父进程
ps aux | awk '$8 ~ /Z/ {print $2, $3}'
# 或
ps -eo pid,ppid,stat,cmd | awk '$3 ~ /Z/'

# 僵尸自己杀不掉（已经死了），要杀它的父进程
kill -CHLD PPID    # 通知父进程有子进程结束了（可能不生效）
kill PPID          # 大杀器：父死后 init 接管，会收尸
```

**根治**：修父进程代码——`fork()` 后一定 `wait()`；或忽略 SIGCHLD（`signal(SIGCHLD, SIG_IGN)`）。

### Q11: 怎么找到磁盘上最大的文件？

```bash
# 方法 1：递归找大文件
find / -type f -size +1G 2>/dev/null -exec ls -lh {} \;
#              ↑ 大于 1GB
#                                      ↑ 详细列出

# 方法 2：按目录汇总
du -sh /var/* 2>/dev/null | sort -rh | head -10

# 方法 3：交互式（推荐）
ncdu /                # 需要装 ncdu，界面友好，按空间可视化
```

**追问：磁盘满了但 `df` 说还有空间？**

- **inode 满了**：文件太多，用 `df -i` 检查
- **有已删除但未释放的文件**（进程还持有 fd）：

```bash
# 找出被删除但仍占空间的文件
sudo lsof | grep deleted
# 输出示例：
# java  1234  root  15w  REG  8,1  10000000  ...  /var/log/app.log (deleted)
#                                                                     ↑ 已删但被占

# 处理：重启该进程，或 truncate 那个 fd
sudo truncate -s 0 /proc/1234/fd/15
```

### Q12: 系统被打满了，怎么快速找到大文件写入源？

```bash
# 实时监控 IO 大户
sudo iotop -oPa

# 找刚才增长最快的文件
sudo find /var/log -type f -mmin -5 -size +100M
#                          ↑ 5 分钟内改过

# 用 fatrace 实时看文件被谁改
sudo apt install fatrace
sudo fatrace
# 输出实时的 process → file open/write 事件
```

---

## Part 4：权限与账号

### Q13: 755 / 644 / 600 什么意思？

**数字权限速算**：`r=4, w=2, x=1`，每位相加。

| 数字 | 权限 | 场景 |
|-----|------|-----|
| 755 | rwxr-xr-x | 目录、可执行脚本 |
| 644 | rw-r--r-- | 普通文件（配置、文档） |
| 600 | rw------- | SSH 私钥、敏感文件 |
| 700 | rwx------ | 只有自己能进的目录（如 ~/.ssh） |
| 777 | rwxrwxrwx | ⚠️ 极大安全隐患，禁用 |

**三段含义**：`属主 / 属组 / 其他人`。

**追问：目录的 x 权限是干什么的？**

- 目录的 `r` = 能 `ls`
- 目录的 `w` = 能在里面创建/删除文件
- 目录的 `x` = 能**进入**（`cd`）+ 访问里面的文件

**只给 r 没给 x 的目录，你能 `ls` 但看不了任何文件的详情**（因为 `stat` 需要 x）。

### Q14: `sudo` 和 `su` 区别？

- `su`：切换用户（Switch User），默认切 root，需要 root 密码
- `su -`：切换并加载目标用户的环境变量（推荐）
- `sudo`：以 root 权限**执行单条命令**，用**自己的密码**（管理员在 `/etc/sudoers` 授权）

生产上：
- 禁用 root 直接 SSH（`PermitRootLogin no`）
- 所有人用普通账号 + sudo，可审计

### Q15: 加个用户并给 sudo 权限

```bash
# 1. 创建用户
sudo useradd -m -s /bin/bash jake
#            ↑ 创建 home 目录  ↑ 指定 shell

# 2. 设密码
sudo passwd jake

# 3. 加入 sudo 组
sudo usermod -aG sudo jake            # Debian/Ubuntu
sudo usermod -aG wheel jake           # RHEL/CentOS
#            ↑ -a 追加  -G 组

# 4. 验证
groups jake
# 输出：jake : jake sudo
```

---

## Part 5：进程与信号

### Q16: 常见信号有哪些？

| 编号 | 名字 | 含义 | 能否被捕获 |
|-----|------|------|-----------|
| 1 | SIGHUP | 终端挂起，很多守护进程用它 reload 配置 | ✅ |
| 2 | SIGINT | Ctrl+C | ✅ |
| 3 | SIGQUIT | Ctrl+\，退出并 core dump | ✅ |
| 9 | SIGKILL | 强杀 | ❌ **不能** |
| 15 | SIGTERM | 优雅退出请求（默认） | ✅ |
| 17 | SIGCHLD | 子进程结束通知 | ✅ |
| 18 | SIGCONT | 继续（stopped 进程） | ✅ |
| 19 | SIGSTOP | 暂停 | ❌ **不能** |
| 20 | SIGTSTP | Ctrl+Z（暂停） | ✅ |

**记忆点**：
- **9 (KILL) 和 19 (STOP) 无法被捕获**——所以 `kill -9` 是终极手段
- 优雅停止用 `kill 15`，等几秒再 `kill 9`

### Q17: 前台 / 后台 / nohup 的差别？

```bash
./server                # 前台跑，占终端，Ctrl+C 结束

./server &              # 后台跑，占终端 SIGHUP 时会死（关终端就没）

nohup ./server &        # 后台跑，忽略 SIGHUP，关终端也活着
# 但 stdout/stderr 默认写到 nohup.out

nohup ./server > out.log 2>&1 &   # 完整生产写法

# 优雅：用 systemd / supervisor 才是正解
```

### Q18: crontab 语法与坑

```
* * * * * command
│ │ │ │ │
│ │ │ │ └── 星期 (0-7，0 和 7 都是周日)
│ │ │ └──── 月 (1-12)
│ │ └────── 日 (1-31)
│ └──────── 时 (0-23)
└────────── 分 (0-59)
```

**例子**：

```bash
crontab -e     # 编辑

# 每分钟
* * * * * /path/to/script.sh

# 每天凌晨 2:30
30 2 * * * /path/to/backup.sh

# 每 5 分钟
*/5 * * * * /path/to/check.sh

# 每周一 3:00
0 3 * * 1 /path/to/weekly.sh

# 工作日每小时
0 * * * 1-5 /path/to/hourly.sh
```

**三大坑**：

1. **PATH 不同**：cron 只用最小 PATH，脚本里所有命令用绝对路径或先 `source /etc/profile`
2. **默认无输出**：脚本报错你看不到，加 `>> /var/log/mycron.log 2>&1`
3. **重复执行**：脚本跑得比周期长会重叠，用 flock 加互斥锁：

```bash
* * * * * flock -n /tmp/mycron.lock /path/to/script.sh
#                 ↑ 非阻塞加锁，拿不到就退出
```

---

## Part 6：文件系统与磁盘

### Q19: 软链接 vs 硬链接

| | 软链接（symlink） | 硬链接 |
|---|---|---|
| 命令 | `ln -s target link` | `ln target link` |
| 本质 | 指向路径的快捷方式 | 指向同一 inode |
| 跨文件系统 | ✅ | ❌ 不行 |
| 目录 | ✅ 可以 | ❌ 不行（避免循环） |
| 目标删除后 | ⚠️ 变悬空 | ✅ 数据还在 |
| inode 数 | 独立 | 共享 |

**验证**：

```bash
$ echo "hello" > file.txt
$ ln file.txt hard.txt       # 硬链接
$ ln -s file.txt soft.txt    # 软链接

$ ls -li
1234  file.txt        ← inode 1234
1234  hard.txt        ← 同 inode，共享数据
5678  soft.txt -> file.txt   ← 独立 inode，只是引用路径

$ rm file.txt
$ cat hard.txt         # hello（数据还在）
$ cat soft.txt         # 报错 No such file
```

### Q20: 挂载磁盘的完整流程

```bash
# 1. 看新盘
lsblk
# NAME   SIZE MOUNTPOINT
# sda    100G /
# sdb    500G           ← 新盘，未挂载

# 2. 分区（sdb 整个用一个分区）
sudo parted /dev/sdb mklabel gpt
sudo parted /dev/sdb mkpart primary ext4 0% 100%

# 3. 格式化
sudo mkfs.ext4 /dev/sdb1

# 4. 挂载点
sudo mkdir /data

# 5. 手动挂载（临时）
sudo mount /dev/sdb1 /data

# 6. 永久挂载：写 /etc/fstab
sudo blkid /dev/sdb1
# /dev/sdb1: UUID="abc-def-..." TYPE="ext4"

# 编辑 /etc/fstab，加一行：
UUID=abc-def-... /data ext4 defaults 0 2
#                                     ↑ dump 备份标志
#                                       ↑ fsck 顺序（根用 1，其他 2）

# 验证 fstab 语法
sudo mount -a

# 7. 卸载
sudo umount /data
```

**追问：挂载点里已有内容会怎样？**

会**被挂载后的文件系统盖住**（原文件还在磁盘上，卸载后能看到）。挂载前记得确认目录为空。

---

## Part 7：网络

### Q21: TCP 三次握手 / 四次挥手

```
三次握手（建立）：

  Client              Server
    │                    │
    │──── SYN, seq=x ───▶│           SYN-SENT / LISTEN
    │                    │
    │◀── SYN-ACK, seq=y ─│           SYN-RECV
    │        ack=x+1     │
    │                    │
    │───── ACK, ack=y+1 ─▶           ESTABLISHED
    │                    │

四次挥手（关闭）：

  Client              Server
    │                    │
    │──── FIN ──────────▶│           FIN-WAIT-1 / CLOSE-WAIT
    │                    │
    │◀────── ACK ────────│           FIN-WAIT-2
    │                    │
    │◀────── FIN ────────│           TIME-WAIT / LAST-ACK
    │                    │
    │─────── ACK ───────▶│           TIME-WAIT (2MSL=60s) / CLOSED
    │        (2MSL 后)    │           CLOSED
```

**为什么建立 3 次、关闭 4 次**：
- 建立：SYN 和 ACK 能合并成一个包
- 关闭：**被动方收到 FIN 后可能还有数据要发**，先 ACK 后 FIN 分开

**为什么 TIME-WAIT 要等 2MSL**：
- 确保对方能收到最后的 ACK（如果丢了，对方会重发 FIN）
- 让本次连接的报文在网络里全部消失，避免下个连接串包

### Q22: DNS 解析流程 & 排查

```
浏览器 → 系统 DNS 缓存 → hosts 文件 → 本地 DNS 服务器
     → 根 DNS → 顶级 DNS (.com) → 权威 DNS → 返回 IP
```

**排查工具**：

```bash
# 看本地 hosts
cat /etc/hosts

# 看用哪个 DNS
cat /etc/resolv.conf
# nameserver 8.8.8.8

# 逐层查询
dig example.com
dig +trace example.com          # 完整解析路径

# 只看 IP
dig +short example.com

# 反向解析（IP → 域名）
dig -x 8.8.8.8

# 指定 DNS 服务器
dig @8.8.8.8 example.com

# 老工具（nslookup 有时可用，格式不同）
nslookup example.com
```

**常见问题**：
- DNS 慢 → 换 DNS 服务器（`8.8.8.8` / `1.1.1.1`）或本地缓存（nscd / systemd-resolved）
- 解析结果错 → 检查 `/etc/hosts`、`/etc/nsswitch.conf`
- 内网 DNS 挂了 → `dig +short @公网DNS` 排除

### Q23: 抓包排查具体问题

```bash
# 抓某接口 & 某端口
sudo tcpdump -i eth0 -nn -A port 8080
#            ↑ 网卡    ↑ 不解析     ↑ 端口
#                              ↑ ASCII 打印 payload

# 只看 SYN 包（判断连接是否成功）
sudo tcpdump -i eth0 -nn 'tcp[tcpflags] & tcp-syn != 0'

# 只看 RST（连接被拒的证据）
sudo tcpdump -i eth0 -nn 'tcp[tcpflags] & tcp-rst != 0'

# 存包给 wireshark 分析
sudo tcpdump -i eth0 -w cap.pcap host 10.0.0.5

# 用 tshark 命令行分析 pcap
tshark -r cap.pcap -Y "tcp.analysis.retransmission"     # 看重传
tshark -r cap.pcap -q -z conv,tcp                        # TCP 会话汇总
```

**追问：应用报连接超时，抓包怎么定位是网络还是应用？**

- 看到 SYN 但没 SYN-ACK → **对端未监听或防火墙拦了**
- SYN-ACK 有但 ACK 后没数据 → **应用没读**
- 数据发出去但 ack 慢 → **网络延迟**
- 大量重传 → **丢包严重**
- 突然 RST → **对端 crash 或防火墙 reset**

---

## 收尾：Cheatsheet

**5 分钟能默写完这些的话，Linux 基础面试基本没问题**：

```bash
# 每秒 CPU 状态
mpstat -P ALL 1

# 内存
free -h && vmstat 1 3

# 磁盘 IO
iostat -xz 1

# 网络连接
ss -tan | awk 'NR>1 {print $1}' | sort | uniq -c

# CPU 大户
top -o %CPU

# 内存大户
ps aux --sort=-%mem | head -10

# 找大文件
du -sh /* 2>/dev/null | sort -rh

# 抓包
sudo tcpdump -i eth0 -nn port 8080

# 系统调用
sudo strace -p PID -e trace=network

# Nginx 日志分析
awk '{print $9}' access.log | sort | uniq -c | sort -rn

# 60 秒诊断
uptime; dmesg | tail; vmstat 1; mpstat -P ALL 1; \
pidstat 1; iostat -xz 1; free -m; sar -n DEV 1; sar -n TCP,ETCP 1
```

---

## 相关文档

- [commands.md](./commands.md) — 命令实战详解
- [perf-tools.md](./perf-tools.md) — 性能工具箱
- [troubleshooting.md](./troubleshooting.md) — 生产问题排查
