# 二分查找（Binary Search）

**标签**: #algorithm #searching #基础 #高频
**难度**: ⭐⭐
**时间复杂度**: O(log n)
**空间复杂度**: 迭代 O(1) / 递归 O(log n)（栈空间）
**前提条件**: 数据必须**有序**

---

## 核心思想

在有序数组中，每次取中间元素与目标比较，根据大小关系将搜索区间**折半**：

- `arr[mid] == target` → 找到，返回 mid
- `arr[mid] < target` → 目标在右半部分，`left = mid + 1`
- `arr[mid] > target` → 目标在左半部分，`right = mid - 1`

每次淘汰一半的数据，最多比较 `⌈log₂(n)⌉` 次。

## 动画示意

```
数组: [1, 3, 5, 7, 9, 11, 13, 15, 17]     查找 target = 13
下标:  0  1  2  3  4  5   6   7   8

第1次: left=0, right=8, mid=4, arr[4]=9  < 13 → left = 5
       [_, _, _, _, _, 11, 13, 15, 17]
                       ↑
第2次: left=5, right=8, mid=6, arr[6]=13 == 13 → 命中，返回 6
                            ↑
```

## Go 实现（标准版）

```go
package main

import "fmt"

// binarySearch 在升序数组 arr 中查找 target 的下标。
// 找到返回下标，未找到返回 -1。
//
// 核心边界：
//   - 使用闭区间 [left, right]，因此循环条件是 left <= right
//   - mid 命中后立即返回
//   - 未命中时缩小区间：mid 已经比较过，下次要排除它，用 mid+1 / mid-1
func binarySearch(arr []int, target int) int {
    left, right := 0, len(arr)-1

    // 闭区间 [left, right] 非空的条件是 left <= right
    for left <= right {
        // 关键：用 left + (right-left)/2 而不是 (left+right)/2
        // 防止 left+right 溢出（在 int32 或大数据量下）
        mid := left + (right-left)/2

        switch {
        case arr[mid] == target:
            return mid
        case arr[mid] < target:
            // 目标在右半部分，且 mid 已比较过，从 mid+1 开始
            left = mid + 1
        default:
            // 目标在左半部分，且 mid 已比较过，到 mid-1 结束
            right = mid - 1
        }
    }

    return -1
}

func main() {
    arr := []int{1, 3, 5, 7, 9, 11, 13, 15, 17}
    fmt.Println(binarySearch(arr, 13))  // 6
    fmt.Println(binarySearch(arr, 4))   // -1
}
```

## Go 实现（递归版）

```go
// binarySearchRecursive 递归版本二分查找。
// 参数 left, right 为当前搜索区间的闭区间边界。
func binarySearchRecursive(arr []int, target, left, right int) int {
    // 递归终止：区间为空
    if left > right {
        return -1
    }

    mid := left + (right-left)/2

    switch {
    case arr[mid] == target:
        return mid
    case arr[mid] < target:
        return binarySearchRecursive(arr, target, mid+1, right)
    default:
        return binarySearchRecursive(arr, target, left, mid-1)
    }
}
```

## PHP 实现

```php
<?php

function binarySearch(array $arr, int $target): int
{
    $left = 0;
    $right = count($arr) - 1;

    while ($left <= $right) {
        $mid = $left + intdiv($right - $left, 2);

        if ($arr[$mid] === $target) {
            return $mid;
        } elseif ($arr[$mid] < $target) {
            $left = $mid + 1;
        } else {
            $right = $mid - 1;
        }
    }

    return -1;
}

$arr = [1, 3, 5, 7, 9, 11, 13, 15, 17];
echo binarySearch($arr, 13);  // 6
```

---

## 四大变体（面试高频）

变体的核心区别在**边界收缩策略**——命中后不立即返回，而是继续向一边收缩。

### 变体 1：第一个等于 target 的位置（左边界）

```go
// firstEqual 返回第一个等于 target 的下标，不存在返回 -1。
// 适用于数组有重复元素时定位第一个匹配位置。
func firstEqual(arr []int, target int) int {
    left, right := 0, len(arr)-1
    result := -1

    for left <= right {
        mid := left + (right-left)/2
        if arr[mid] == target {
            result = mid       // 记录候选，继续向左找更小的下标
            right = mid - 1
        } else if arr[mid] < target {
            left = mid + 1
        } else {
            right = mid - 1
        }
    }

    return result
}
```

### 变体 2：最后一个等于 target 的位置（右边界）

```go
func lastEqual(arr []int, target int) int {
    left, right := 0, len(arr)-1
    result := -1

    for left <= right {
        mid := left + (right-left)/2
        if arr[mid] == target {
            result = mid       // 记录候选，继续向右找更大的下标
            left = mid + 1
        } else if arr[mid] < target {
            left = mid + 1
        } else {
            right = mid - 1
        }
    }

    return result
}
```

### 变体 3：第一个大于等于 target 的位置（lower_bound）

C++ STL 的 `lower_bound`，实际工程中最常用：

```go
// firstGreaterOrEqual 返回第一个 >= target 的下标，全都小于则返回 len(arr)。
// 常用于"插入位置"、"计数区间内的元素个数"等场景。
func firstGreaterOrEqual(arr []int, target int) int {
    left, right := 0, len(arr)-1
    result := len(arr)   // 默认所有元素都小于 target

    for left <= right {
        mid := left + (right-left)/2
        if arr[mid] >= target {
            result = mid       // 候选，继续向左找更早的位置
            right = mid - 1
        } else {
            left = mid + 1
        }
    }

    return result
}
```

### 变体 4：最后一个小于等于 target 的位置（upper_bound 前一位）

```go
// lastLessOrEqual 返回最后一个 <= target 的下标，全都大于则返回 -1。
func lastLessOrEqual(arr []int, target int) int {
    left, right := 0, len(arr)-1
    result := -1

    for left <= right {
        mid := left + (right-left)/2
        if arr[mid] <= target {
            result = mid       // 候选，继续向右找更后的位置
            left = mid + 1
        } else {
            right = mid - 1
        }
    }

    return result
}
```

### 四个变体统一记忆

| 变体 | 命中时怎么办 | 未命中时 |
|---|---|---|
| 找任意一个等于 | 立即返回 | 左/右缩小 |
| 第一个等于 | 记录并向**左**继续 | 左/右缩小 |
| 最后一个等于 | 记录并向**右**继续 | 左/右缩小 |
| 第一个 ≥ | `>=` 记录并向**左** | `<` 向右 |
| 最后一个 ≤ | `<=` 记录并向**右** | `>` 向左 |

**记忆口诀**：找第一个就往左收，找最后一个就往右收。

---

## 边界条件深度解析

二分查找容易写错的三个点：

### 1. 循环条件：`<=` 还是 `<`？

取决于区间定义：

| 区间定义 | 循环条件 | 收缩方式 |
|---|---|---|
| **闭区间 [left, right]** | `left <= right` | `left = mid+1`, `right = mid-1` |
| **左闭右开 [left, right)** | `left < right` | `left = mid+1`, `right = mid` |

**推荐用闭区间**（第一种），语义更直观，不容易漏 case。

### 2. mid 计算：为什么用 `left + (right-left)/2`？

```go
mid := (left + right) / 2         // ❌ 可能溢出
mid := left + (right-left)/2      // ✅ 安全
```

在 Java 或 C 中，`left + right` 可能超出 int 范围（比如两者都接近 `INT_MAX`）。
Go 的 int 通常是 64 位，风险小，但保持这个写法是**跨语言最佳实践**。

### 3. 更新时用 `mid + 1` 还是 `mid`？

**闭区间下必须用 `mid + 1` / `mid - 1`**：
- mid 已经比较过，不可能是答案
- 若写成 `right = mid`，可能死循环

死循环示例：
```
[1, 2]  查找 3
left=0, right=1, mid=0, arr[0]=1 < 3 → left = mid + 1 = 1 ✓
left=1, right=1, mid=1, arr[1]=2 < 3 → left = mid + 1 = 2 ✓ 循环结束

若错写成 left = mid：
left=0, right=1, mid=0, arr[0]=1 < 3 → left = mid = 0  ❌ 死循环
```

---

## 常见错误

| 错误写法 | 问题 |
|---|---|
| `while left < right` + `right = mid - 1` | 会漏掉最后一个元素 |
| `mid = (left + right) / 2` | 大数下溢出 |
| `right = mid`（闭区间下） | 死循环 |
| 忘记数组必须有序 | 结果不可预测 |
| 用 float 存索引 | 不必要且容易精度问题 |

---

## 适用场景

- 有序数组精确/范围查找
- 查找**第一个满足某条件**的位置（判定函数单调）
- 在旋转有序数组、山脉数组等变形中找特定值
- 求解满足某单调性质的**最小/最大值**（"二分答案"）
- 数据库 B+ 树内部节点定位、跳表加速

## 不适用场景

- 数据无序（要先排序，O(n log n) + O(log n) 反而不如线性扫描）
- 频繁插入删除的动态数据（用平衡树/跳表更合适）
- 数据量极小（< 20 时线性扫描的常数更优）
- 链表结构（无法 O(1) 访问 mid）

---

## 面试追问

### Q1: 时间复杂度为什么是 O(log n)？

每次比较后搜索区间长度减半。设 n 个元素最多比较 k 次：
```
n → n/2 → n/4 → ... → 1
   2^k = n  →  k = log₂ n
```

### Q2: 二分查找的前提是什么？

**数据必须有序，且支持 O(1) 随机访问**（数组、切片可以；链表不行）。

### Q3: 有序数组的插入位置怎么找？

用**变体 3**（第一个大于等于 target 的位置），即 `lower_bound`。返回值就是应该插入的下标。

### Q4: 旋转有序数组（如 [4,5,6,7,0,1,2]）中查找目标值？

变形二分：先判断 `mid` 落在哪一段（左段或右段），再判断 target 属于哪一段：

```go
func searchRotated(arr []int, target int) int {
    left, right := 0, len(arr)-1
    for left <= right {
        mid := left + (right-left)/2
        if arr[mid] == target {
            return mid
        }
        // 判断哪一段有序
        if arr[left] <= arr[mid] {
            // 左半段有序
            if arr[left] <= target && target < arr[mid] {
                right = mid - 1
            } else {
                left = mid + 1
            }
        } else {
            // 右半段有序
            if arr[mid] < target && target <= arr[right] {
                left = mid + 1
            } else {
                right = mid - 1
            }
        }
    }
    return -1
}
```

### Q5: 什么是"二分答案"？

不是在数组里找元素，而是**在答案空间上二分**。例：
- "分配货物给 K 个仓库，最小化最大仓库负载"
- "在数轴上摆放 N 个牛，两两间距的最大最小值"

判定函数 `canDo(x)` 单调（x 越大越容易满足），就可以对 x 二分。核心模板：

```go
left, right := minAnswer, maxAnswer
for left < right {
    mid := left + (right-left)/2
    if canDo(mid) {
        right = mid       // 满足条件，尝试更小的 mid
    } else {
        left = mid + 1
    }
}
return left
```

### Q6: 二分查找和哈希查找的区别？

| 维度 | 二分查找 | 哈希查找 |
|---|---|---|
| 时间复杂度 | O(log n) | O(1) 平均 |
| 空间复杂度 | O(1) | O(n) |
| 是否要有序 | 必须 | 不需要 |
| 支持范围查询 | ✅ | ❌ |
| 内存局部性 | 好（数组连续） | 差（散列） |

哈希更快但只能精确查找；二分慢一点但能做范围/最近邻查询。

### Q7: Go 标准库有二分查找吗？

有，`sort` 包提供：

```go
// sort.SearchInts 是 lower_bound 语义
i := sort.SearchInts(arr, target)
if i < len(arr) && arr[i] == target {
    // 找到
}

// 通用版本
i := sort.Search(len(arr), func(i int) bool {
    return arr[i] >= target
})
```

**面试写题一般还是手写**，展示对边界的掌握。工程中优先用标准库。

### Q8: 如何判断"能否二分"？

看是否存在**单调性质**：存在某个位置 p，使得 `[0, p)` 和 `[p, n)` 分别有一致的判定结果。哪怕数组本身不有序，只要判定函数单调，就能二分。
