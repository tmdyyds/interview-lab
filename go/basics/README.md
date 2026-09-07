# Go 基础

Go 语言基础知识全覆盖：数据类型、变量、控制流、函数、结构体、接口、包管理、错误处理、核心陷阱。

## 目录

- [data-types.md](./data-types.md) — 数据类型分类：值类型/引用类型/零值/类型转换
- [variables.md](./variables.md) — 变量/常量/iota/作用域/短声明陷阱
- [functions.md](./functions.md) — 函数/闭包/defer/panic/recover/init
- [structs.md](./structs.md) — 结构体/方法/嵌入/组合/Tag
- [interfaces.md](./interfaces.md) — 接口/鸭子类型/类型断言/空接口/nil陷阱
- [slices-maps.md](./slices-maps.md) — slice/map 底层原理与常见坑
- [packages.md](./packages.md) — 包管理/import/init执行顺序/go modules
- [error-handling.md](./error-handling.md) — error 设计哲学/自定义错误/errors 包/最佳实践
- [pointers.md](./pointers.md) — 指针/值传递/逃逸分析
- [control-flow.md](./control-flow.md) — for/range/select/switch/goto/label

## 常考重点

- slice 底层结构与扩容规则
- map 并发不安全 + 遍历随机性
- interface 底层（iface/eface）+ nil 陷阱
- defer 执行顺序 + 闭包捕获
- init 函数执行顺序
- 值接收者 vs 指针接收者
- for range 循环变量（Go 1.22 前后差异）
- error 包装与解包（`%w` / `errors.Is` / `errors.As`）

## 面试高频索引

见 [../questions.md](../questions.md) 基础部分
