# Linux 常用命令实战

**标签**: #linux #commands #shell #高频

按 **文本处理 / 文件搜索 / 权限 / 进程 / 网络** 五大类整理。每条都带例子和逐行注释。

---

## 一、文本处理

### 1) grep — 按模式搜索

```bash
# 基础：在文件里找 "error"（大小写敏感）
grep "error" app.log

# -i    忽略大小写
# -n    显示行号
# -r    递归目录
# -w    完全匹配单词（不匹配 password 里的 pass）
# -v    反向匹配（不包含）
# -c    只输出匹配行数
# -A 3  匹配行 + 后 3 行
# -B 3  匹配行 + 前 3 行
# -C 3  匹配行 + 前后各 3 行
grep -inrw "error" /var/log/

# 多模式：或（-E 扩展正则）
grep -E "error|fatal|panic" app.log

# 排除模式
grep -v "healthcheck" access.log

# 只显示匹配的部分（不是整行）
grep -oE "[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+" access.log | sort -u
#          ↑ IP 地址正则
#                             ↑ sort -u 去重
```

**真实输出示例**：

```bash
$ grep -n "ERROR" app.log
42:2026-09-22 10:15:03 ERROR: connection refused
88:2026-09-22 10:16:44 ERROR: timeout
# 42 = 行号
# 后面 = 匹配到的整行内容
```

### 2) awk — 按列处理

**核心心法**：`awk '模式 {动作}' 文件`；默认按空白分隔字段，`$0` 是整行，`$1/$2/...` 是列。

```bash
# 打印第 1 列和第 3 列
awk '{print $1, $3}' access.log

# 用逗号分隔的 CSV
awk -F',' '{print $2}' users.csv
#     ↑ -F 指定分隔符

# 条件过滤 + 计数
awk '$9 == 500 {count++} END {print count}' access.log
#    ↑ Nginx 日志第 9 列是状态码，等于 500 才计数
#                              ↑ END 块在文件读完后执行

# 每个状态码出现次数（面试高频）
awk '{codes[$9]++} END {for (c in codes) print c, codes[c]}' access.log
# 输出：
# 200 15234
# 404 421
# 500 18

# 求平均值
awk '{sum += $5} END {print sum/NR}' latency.log
#    ↑ 第 5 列累加       ↑ NR = 总行数

# 匹配后加工
awk '/ERROR/ {print $1, $2, "→", $NF}' app.log
#    ↑ 只处理含 ERROR 的行
#                        ↑ $NF = 最后一列
```

### 3) sed — 流式编辑

```bash
# 替换：s/旧/新/
sed 's/foo/bar/' file           # 每行第一处
sed 's/foo/bar/g' file          # 全部（g = global）
sed 's/foo/bar/gi' file         # 忽略大小写

# 原地修改（危险！先备份）
sed -i.bak 's/foo/bar/g' config.yml
#     ↑ 备份为 config.yml.bak

# 删除行
sed '5d' file                   # 删除第 5 行
sed '/^$/d' file                # 删除空行（^ 行首，$ 行尾，^$ = 空行）
sed '/^#/d' config              # 删除注释行

# 只打印匹配行（相当于 grep）
sed -n '/ERROR/p' app.log
#    ↑ -n 抑制默认输出   ↑ p = print

# 只输出第 100-200 行
sed -n '100,200p' huge.log
```

**避坑**：`sed -i` 在 macOS/BSD 需要 `sed -i '' 's/../../'`（空字符串备份后缀），Linux 是 `sed -i 's/../../'`。

### 4) cut — 按列切

```bash
# -d 分隔符   -f 取哪一列
cut -d':' -f1 /etc/passwd       # 取用户名（passwd 用 : 分隔）
# 输出：root / daemon / bin / ...

cut -d',' -f1,3 users.csv       # 取第 1 和第 3 列
cut -d',' -f2-4 users.csv       # 取第 2 到第 4 列

# 按字符位置切
cut -c1-5 file                  # 每行前 5 个字符
```

**awk vs cut**：cut 只能定长切；awk 更灵活但慢一点。**日常按空白切用 awk，按固定分隔符切两三列用 cut**。

### 5) sort — 排序

```bash
sort file                        # 字母序升序
sort -r file                     # 降序
sort -n file                     # 按数字排序（1、2、10 而不是 1、10、2）
sort -k 2 file                   # 按第 2 列排
sort -t',' -k 3 -n users.csv     # CSV 按第 3 列数字排
#      ↑ 分隔符
sort -u file                     # 去重（等价于 sort | uniq）
```

### 6) uniq — 去重

**必须先 sort 才能用**（因为 uniq 只对**相邻行**去重）：

```bash
sort log | uniq                  # 去重
sort log | uniq -c               # 计数（多少次出现）
sort log | uniq -c | sort -rn    # 按次数降序（面试高频组合）

# 输出：
#    523 GET /api/users
#    421 GET /api/orders
#     18 POST /api/pay
```

### 7) wc — 计数

```bash
wc file          # 行数 单词数 字符数 文件名
wc -l file       # 只统计行数
wc -c file       # 字节数
wc -w file       # 单词数

# 常用：计算日志行数
wc -l /var/log/nginx/access.log
```

### 组合技：Nginx 日志分析一行流

```bash
# 面试神题：统计各状态码出现次数，从多到少排
awk '{print $9}' access.log | sort | uniq -c | sort -rn
#    ↑ 取第 9 列       ↑ 排序   ↑ 计数    ↑ 按次数降序
# 输出：
#   15234 200
#     421 404
#      18 500

# TOP 10 访问最多的 IP
awk '{print $1}' access.log | sort | uniq -c | sort -rn | head -10

# TOP 10 慢请求 URL（假设 $NF 是响应时间）
awk '{print $NF, $7}' access.log | sort -rn | head -10
```

---

## 二、文件搜索

### 1) find — 实时搜索（最强）

```bash
# 基础
find /path -name "*.log"          # 按文件名（支持通配符）
find /path -iname "*.LOG"         # 忽略大小写
find /path -type f                # 只找文件（d = 目录，l = 链接）

# 按时间
find /path -mtime -1              # 24 小时内修改过（-mtime -N = N 天内）
find /path -mtime +7              # 7 天前修改的
find /path -mmin -30              # 30 分钟内修改的

# 按大小
find /path -size +100M            # 大于 100MB
find /path -size -1k              # 小于 1KB

# 按权限/owner
find /path -perm 644              # 权限精确匹配
find /path -user jake             # 属主
find /path -group www             # 属组

# 复合条件（-and 是默认，可省略）
find /var/log -name "*.log" -size +100M -mtime +7

# 对结果执行命令：-exec
find /tmp -name "*.tmp" -exec rm {} \;
#                                 ↑ {} 是占位符     ↑ 结束符必须转义

# 更高效：-exec 加 + 号，批量执行
find /tmp -name "*.tmp" -exec rm {} +
# 等价于 rm file1 file2 file3... 一次搞定，比 \; 每个文件调一次快得多

# 删除空目录（自底向上）
find /path -type d -empty -delete
```

**真实场景**：

```bash
# 清理 7 天前的日志
find /var/log/app -name "*.log" -mtime +7 -delete

# 找占空间大的文件（磁盘满时排查）
find / -type f -size +500M 2>/dev/null   # 忽略 permission denied
```

### 2) locate — 数据库快速搜索

```bash
# 基于预建索引，比 find 快 100x，但可能不是最新
locate nginx.conf

# 更新索引（root 或 cron 定时）
sudo updatedb
```

**find vs locate**：
- 实时准确用 `find`
- 全盘快速定位已知文件用 `locate`

### 3) which / whereis — 找命令位置

```bash
which python              # 在 PATH 里找可执行文件
# 输出：/usr/bin/python

whereis python            # 找可执行、源码、man 页
# 输出：python: /usr/bin/python /usr/lib/python /usr/share/man/man1/python.1.gz

type -a python            # 更详细，能识别 alias 和 shell 内建
```

---

## 三、权限管理

### 权限模型三件套

```
-rwxr-xr--  1 jake dev 1234 Sep 22 10:00 script.sh
 ↑          ↑ ↑    ↑
 权限       属主  属组
```

**权限字段拆解**：

```
- rwx r-x r--
│  │   │   └── other 其他人   → 只读
│  │   └────── group 属组     → 读 + 执行
│  └────────── owner 属主     → 读 + 写 + 执行
└──────────── 文件类型：- 普通 / d 目录 / l 链接
```

**数字表示法**：r=4, w=2, x=1，加起来：

```
rwx = 7    rw- = 6    r-x = 5    r-- = 4
```

`755` = `rwxr-xr-x` = 属主全权、其他人读执行（**目录和脚本的标配**）
`644` = `rw-r--r--` = 属主读写、其他人只读（**普通文件标配**）
`600` = `rw-------` = 只有属主能读写（**密钥/敏感配置**）

### 1) chmod — 改权限

```bash
# 数字模式（推荐）
chmod 755 script.sh
chmod 644 config.yml
chmod 600 ~/.ssh/id_rsa   # SSH 私钥必须 600，否则 ssh 会拒用

# 符号模式
chmod u+x script.sh       # 给 owner 加执行权限
chmod g-w file            # 去掉 group 写权限
chmod a+r file            # a = all，所有人加读权限

# 递归：目录及所有子文件
chmod -R 755 /var/www/html
```

**⚠️ 常见坑**：`chmod -R 777` 是极大安全问题——所有人可读写执行。生产禁用。

### 2) chown — 改属主

```bash
chown jake file                  # 只改 owner
chown jake:dev file              # owner:group 一起改
chown :dev file                  # 只改 group
chown -R www-data:www-data /var/www/  # 递归
```

### 3) umask — 新建文件的默认权限

```bash
umask                     # 查看当前 umask
# 输出：0022

# 计算：新建文件权限 = 666 - umask，新建目录 = 777 - umask
# umask 022 → 文件 644，目录 755（默认）
# umask 077 → 文件 600，目录 700（更严格，多用户机器建议）

umask 077                 # 临时设置
# 想永久生效写到 ~/.bashrc
```

---

## 四、进程管理

### 1) ps — 进程快照

```bash
# BSD 语法（无 -），常用组合
ps aux
#  a  所有用户的进程
#  u  显示详细（用户、CPU、内存）
#  x  含没有 tty 的进程（后台进程/守护进程）

# UNIX 语法（带 -）
ps -ef
#  -e  所有进程
#  -f  完整格式（含父进程 PPID）

# 特定进程
ps aux | grep nginx
ps -C nginx               # 按进程名精确
```

**ps aux 输出解读**：

```
USER   PID %CPU %MEM    VSZ   RSS TTY  STAT START   TIME COMMAND
www   1234  2.0  1.5 200000 15000 ?    Ss   09:00   1:23 nginx: master
│     │    │    │    │      │    │    │
│     │    │    │    │      │    │    └── S=可中断睡眠 s=session leader
│     │    │    │    │      │    └────── ? = 无控制终端
│     │    │    │    │      └─────────── 实际物理内存 KB（重要！）
│     │    │    │    └────────────────── 虚拟内存 KB
│     │    │    └─────────────────────── 内存占比
│     │    └──────────────────────────── CPU 占比
│     └───────────────────────────────── 进程 ID
└─────────────────────────────────────── 进程属主
```

**STAT 常见值**：
- `R` running（正在跑或在运行队列里）
- `S` sleeping（可中断，比如等 IO）
- `D` uninterruptible sleep（**通常是等磁盘 IO**，杀不掉）
- `Z` zombie（僵尸进程，父进程没 wait）
- `T` stopped（Ctrl+Z 或 SIGSTOP）

### 2) top / htop — 实时监控

```bash
top                       # 交互式，默认按 CPU 排序

# 交互键：
#   P   按 CPU 排序
#   M   按内存排序
#   T   按运行时间排
#   c   显示完整命令
#   1   显示每个 CPU 核心
#   k   kill 进程（输入 PID）
#   q   退出
```

**top 头部信息**：

```
top - 10:30:15 up 30 days,  load average: 1.20, 0.85, 0.60
                                          ↑ 1min  5min  15min
Tasks: 200 total,   2 running, 198 sleeping,   0 zombie
%Cpu(s):  5.2 us,  1.3 sy,  0.0 ni, 93.0 id,  0.5 wa
          ↑ user  ↑ system   ↑ nice ↑ idle  ↑ iowait ← 关注 wa！
KiB Mem : 16000000 total,  2000000 free, 10000000 used
KiB Swap:  4000000 total,  4000000 free,        0 used
```

**load average 判读**：
- 一般规则：`load > CPU 核数` 说明系统繁忙
- 单核 load = 1 意味着 CPU 完全跑满
- 8 核机器 load = 8 才是等价"满载"
- **1min 增长快、5/15min 还低** → 突发；**三个都高** → 持续压力

**htop** 是 top 的美化版（要 `apt install htop` / `yum install htop`），支持鼠标点击、树状进程视图、颜色化 CPU 条。

### 3) kill — 发信号

```bash
kill 1234                 # 默认发 SIGTERM (15)，请求进程优雅退出
kill -9 1234              # SIGKILL，强制杀（不能被捕获，来不及清理资源）
kill -15 1234             # SIGTERM 显式

# 常用信号
# 1  SIGHUP   挂起，nginx / rsyslog 用来重载配置
# 2  SIGINT   Ctrl+C
# 9  SIGKILL  强杀
# 15 SIGTERM  优雅退出（推荐）
# 18 SIGCONT  继续
# 19 SIGSTOP  暂停

# 批量杀
pkill nginx               # 按名字
killall python            # 按名字（GNU），杀所有 python 进程
pkill -f "python worker"  # 按命令行匹配（-f 表示匹配整个命令行）
```

**优雅停止的正确姿势**：先 `kill 15`，等几秒还没退再 `kill 9`。

### 4) nohup / & / disown — 后台运行

```bash
# 后台跑，退出终端后仍继续
nohup ./server > out.log 2>&1 &
# ↑ nohup 忽略 SIGHUP     ↑ 合并 stderr 到 stdout   ↑ 后台

# 已经启动的前台进程转后台
./server                    # 前台跑
Ctrl+Z                      # 挂起
bg                          # 转后台
disown -h %1                # 从 shell 的作业列表移除，退出 shell 不影响

# 生产上更好的选择：systemd / supervisor / tmux
```

---

## 五、网络工具

### 1) netstat / ss — 连接与端口

`netstat` 老命令，`ss` 是新版（更快，socket 更多信息）。**优先用 ss**。

```bash
# 所有 TCP 监听端口 + 进程
ss -tlnp
#  -t   TCP
#  -l   listening
#  -n   数字端口（不做 DNS 反查，更快）
#  -p   显示进程

# 所有 UDP 监听
ss -ulnp

# 所有连接（TCP + UDP）
ss -anp

# 按端口过滤
ss -tnp state established '( dport = :3306 or sport = :3306 )'
#         ↑ 只看 established 状态          ↑ 端口 3306

# 统计各状态连接数
ss -tan | awk 'NR>1 {print $1}' | sort | uniq -c
#            ↑ 跳过表头行
# 输出示例：
#     42 ESTAB
#     18 TIME-WAIT
#      3 LISTEN
```

**TCP 状态速查**：

```
LISTEN       监听中
ESTABLISHED  已建立连接
SYN-SENT     刚发 SYN，等 SYN-ACK
SYN-RECV     收到 SYN 并回了 SYN-ACK
FIN-WAIT-1/2 主动关闭方等待
CLOSE-WAIT   ⚠️ 被动关闭方还没 close，堆积说明代码漏 close
TIME-WAIT    主动关闭方等 2MSL（60s），高并发场景可能堆积
```

### 2) lsof — 打开的文件（含 socket）

```bash
# 谁在监听 8080 端口？
lsof -i :8080

# 某进程打开的所有文件
lsof -p 1234

# 某文件被谁在用（卸载 U 盘前排查）
lsof /mnt/usb

# 找出未关闭的 socket
lsof -i -n | grep ESTABLISHED
#      ↑ 只看 IP socket    ↑ 不做 DNS 解析
```

### 3) tcpdump — 抓包

```bash
# 抓 eth0 上所有流量（Ctrl+C 停）
sudo tcpdump -i eth0

# 只抓某端口
sudo tcpdump -i eth0 port 80

# 抓某主机的流量
sudo tcpdump -i eth0 host 192.168.1.100

# 组合：某主机的 80 端口
sudo tcpdump -i eth0 host 10.0.0.5 and port 80

# 写文件，事后用 wireshark 分析
sudo tcpdump -i eth0 port 3306 -w mysql.pcap
#                                 ↑ -w 写 pcap 文件

# 常用 flag
# -nn   不做端口和主机名解析
# -A    以 ASCII 打印包内容（HTTP 明文可读）
# -X    十六进制 + ASCII
# -c 100  抓 100 个包就退
# -s 0    抓完整包（默认只抓头部）
```

**输出示例**：

```
10:30:15.123 IP 10.0.0.5.54321 > 10.0.0.10.80: Flags [S], seq 1234
             ↑ 源 IP.端口     ↑ 目的 IP.端口   ↑ SYN 包
```

**flag 缩写**：`S` = SYN，`.` = ACK，`F` = FIN，`R` = RST，`P` = PSH。

### 4) curl / wget — HTTP 客户端

```bash
# GET
curl https://example.com

# 显示响应头 + body
curl -i https://example.com

# 只看响应头
curl -I https://example.com
#     ↑ -I 发 HEAD 请求

# 详细模式：请求头、TLS 握手、响应头全打印
curl -v https://example.com

# POST JSON
curl -X POST https://api.example.com/users \
     -H "Content-Type: application/json" \
     -d '{"name":"jake"}'

# 带 cookie
curl -b "session=abc123" https://example.com

# 保存到文件
curl -o page.html https://example.com

# 只测响应时间（性能测试常用）
curl -w "DNS:%{time_namelookup} TCP:%{time_connect} TLS:%{time_appconnect} TTFB:%{time_starttransfer} Total:%{time_total}\n" \
     -o /dev/null -s https://example.com
# 输出：
# DNS:0.012 TCP:0.045 TLS:0.089 TTFB:0.156 Total:0.201
```

**wget vs curl**：
- wget 强在**下载**（断点续传、递归下载整站）
- curl 强在**API 调试**（协议丰富、灵活）

```bash
# wget 常用
wget https://example.com/file.zip           # 下载
wget -c https://example.com/big.iso         # 断点续传
wget -r -np https://docs.example.com/       # 递归下载（-np = 不追父目录）
```

---

## 六、组合技锦囊

### 找到最占空间的目录

```bash
du -sh /var/* 2>/dev/null | sort -rh | head -10
# ↑ 每个子目录汇总大小   ↑ 按人类可读排序（1G > 500M > 100K）
```

### 找到僵尸进程

```bash
ps aux | awk '$8 ~ /Z/'
#         ↑ STAT 列包含 Z 的行
```

### 每秒统计网络新连接数

```bash
watch -n 1 'ss -tan | grep ESTAB | wc -l'
#      ↑ 每 1 秒执行一次
```

### 找到 CPU 用量 > 50% 的进程

```bash
ps aux | awk '$3 > 50 {print $2, $3, $11}'
#            ↑ %CPU 列
```

### Nginx 访问最多的 URL top 10

```bash
awk '{print $7}' access.log | sort | uniq -c | sort -rn | head -10
```

### 找出大文件（> 1GB）

```bash
find / -type f -size +1G 2>/dev/null | xargs -I{} du -h {} | sort -rh
#                        ↑ 忽略权限报错     ↑ 每个文件算大小
```

---

## 七、下一步

- **性能排查工具**：见 [perf-tools.md](./perf-tools.md)
- **系统排查思路**：见 [troubleshooting.md](./troubleshooting.md)
- **面试题**：见 [interview-questions.md](./interview-questions.md)
