# Go 控制流

**标签**: #go #basics #control-flow

---

## 一、for 循环（Go 唯一的循环语句）

```go
// 传统 for
for i := 0; i < 10; i++ { }

// while 风格
for condition { }

// 无限循环
for { }

// for range（遍历 slice/map/string/channel）
for index, value := range slice { }
for key, value := range mapData { }
for index, char := range "hello你好" { }  // char 是 rune
for msg := range ch { }  // 从 channel 读直到 close
```

### for range 陷阱

```go
// Go 1.22 之前：循环变量是同一个（地址不变）
for _, v := range []int{1, 2, 3} {
    go func() {
        fmt.Println(v)  // 全部打印 3（v 是同一个变量）
    }()
}

// Go 1.22+：每次迭代创建新变量（修复了这个问题）
// 但仍需注意低版本兼容

// range map 遍历顺序随机（不要依赖顺序）
// range string 按 rune 遍历（不是 byte）
```

## 二、switch

```go
// Go 的 switch 不需要 break（自动 break）
switch day {
case "Mon", "Tue", "Wed", "Thu", "Fri":
    fmt.Println("Weekday")
case "Sat", "Sun":
    fmt.Println("Weekend")
default:
    fmt.Println("Unknown")
}

// fallthrough（强制执行下一个 case，很少用）
switch n {
case 1:
    fmt.Println("one")
    fallthrough
case 2:
    fmt.Println("two or fallthrough from one")
}

// 无条件 switch（替代 if-else 链）
switch {
case score >= 90:
    grade = "A"
case score >= 80:
    grade = "B"
default:
    grade = "C"
}

// type switch
switch v := x.(type) {
case int:
    fmt.Println("int:", v)
case string:
    fmt.Println("string:", v)
}
```

## 三、select（channel 多路复用）

```go
// select 类似 switch，但用于 channel 操作
select {
case msg := <-ch1:
    fmt.Println("received from ch1:", msg)
case msg := <-ch2:
    fmt.Println("received from ch2:", msg)
case ch3 <- "hello":
    fmt.Println("sent to ch3")
case <-time.After(5 * time.Second):
    fmt.Println("timeout")
default:
    fmt.Println("no channel ready")  // 非阻塞
}

// ⚠️ select 多个 case 同时就绪时随机选一个（不是按顺序）

// 常用模式：超时控制
select {
case result := <-ch:
    return result, nil
case <-ctx.Done():
    return nil, ctx.Err()
}

// 常用模式：非阻塞发送
select {
case ch <- data:
    // 发送成功
default:
    // channel 已满，丢弃或降级
}
```

## 四、goto / label（慎用）

```go
// goto：跳转到指定标签（很少用，用于跳出多层循环或统一错误处理）
func process() error {
    if err := step1(); err != nil {
        goto cleanup
    }
    if err := step2(); err != nil {
        goto cleanup
    }
    return nil

cleanup:
    rollback()
    return errors.New("process failed")
}

// label + break/continue：跳出指定层级的循环
outer:
    for i := 0; i < 10; i++ {
        for j := 0; j < 10; j++ {
            if i*j > 50 {
                break outer  // 跳出外层循环
            }
        }
    }
```

## 五、面试追问

### Q1: Go 为什么只有 for 没有 while/do-while？

简洁设计。`for` 一个关键字覆盖所有循环场景。`for {}` = while true，`for cond {}` = while cond。

### Q2: select 多个 case 同时就绪时怎么选？

**随机选一个**（伪随机）。这是 Go 的设计选择，防止代码依赖 case 顺序，避免某些 channel 饥饿。

### Q3: switch 不写 break 会怎样？

Go 的 switch 默认每个 case 结束后自动 break（和 C 相反）。如果需要穿透到下一个 case，用 `fallthrough`。
