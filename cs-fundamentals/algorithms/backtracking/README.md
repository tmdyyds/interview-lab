# 回溯算法

## 解题模板

```go
func backtrack(path []int, choices []int) {
    if 满足终止条件 {
        result = append(result, copy(path))
        return
    }
    for i, choice := range choices {
        if 不满足约束 { continue }  // 剪枝
        path = append(path, choice)  // 做选择
        backtrack(path, choices)      // 递归
        path = path[:len(path)-1]    // 撤销选择
    }
}
```

## 待收录

- [ ] 全排列 I/II（含重复元素）
- [ ] 组合 / 组合总和 I/II/III
- [ ] 子集 I/II
- [ ] N 皇后
- [ ] 解数独
- [ ] 括号生成
- [ ] 单词搜索
- [ ] 分割回文串
- [ ] 电话号码的字母组合
- [ ] 排列/组合去重技巧

## 面试常问

- 回溯和 DFS 的区别？
- 如何剪枝优化？
- 排列与组合的区别在代码哪里体现？
