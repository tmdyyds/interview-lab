# 算法

面试常考算法，按分类整理。代码实现以 Go 为主，部分附 PHP 版本。

## 目录

- [sorting/](./sorting/README.md) — 排序算法
- [searching/](./searching/README.md) — 查找算法
- [linked-list/](./linked-list/README.md) — 链表
- [tree/](./tree/README.md) — 树
- [dp/](./dp/README.md) — 动态规划
- [backtracking/](./backtracking/README.md) — 回溯
- [graph/](./graph/README.md) — 图算法
- [string-algo/](./string-algo/README.md) — 字符串算法
- [bit-manipulation/](./bit-manipulation/README.md) — 位运算
- [greedy/](./greedy/README.md) — 贪心

## 常考 Top 题型

| 类型 | 高频题 |
|------|--------|
| 排序 | 快排、归并、堆排序、Top K |
| 查找 | 二分查找及变体 |
| 链表 | 反转、合并、环检测、LRU |
| 树 | 遍历、LCA、BST 验证、层序 |
| DP | 背包、LIS、LCS、爬楼梯、零钱兑换 |
| 回溯 | 全排列、组合、子集、N 皇后 |
| 图 | BFS/DFS、拓扑排序、最短路径 |
| 字符串 | KMP、滑动窗口、回文 |

## 复杂度速查

| 排序算法 | 平均 | 最坏 | 空间 | 稳定 |
|----------|------|------|------|------|
| 冒泡 | O(n²) | O(n²) | O(1) | ✅ |
| 选择 | O(n²) | O(n²) | O(1) | ❌ |
| 插入 | O(n²) | O(n²) | O(1) | ✅ |
| 希尔 | O(n^1.3) | O(n²) | O(1) | ❌ |
| 归并 | O(nlogn) | O(nlogn) | O(n) | ✅ |
| 快排 | O(nlogn) | O(n²) | O(logn) | ❌ |
| 堆排序 | O(nlogn) | O(nlogn) | O(1) | ❌ |
| 计数 | O(n+k) | O(n+k) | O(k) | ✅ |
| 桶排序 | O(n+k) | O(n²) | O(n+k) | ✅ |
| 基数 | O(d·n) | O(d·n) | O(n+k) | ✅ |
