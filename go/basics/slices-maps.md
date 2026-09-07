# Go Slice 与 Map

**标签**: #go #basics #slice #map #高频

---

## 一、Slice 底层结构

```go
// runtime/slice.go
type slice struct {
    array unsafe.Pointer  // 指向底层数组
    len   int             // 当前元素个数
    cap   int             // 底层数组容量
}
// slice 变量本身 = 24 字节（指针8 + len8 + cap8）
```

```
slice s = {array: 0xc0001, len: 3, cap: 5}

底层数组：
 index:  0    1    2    3    4
value: [10] [20] [30] [  ] [  ]
        ↑              ↑         ↑
      array          len=3     cap=5
```

## 二、Slice 扩容规则

```go
// Go 1.18+ 扩容规则（src/runtime/slice.go growslice）：
// 1. 如果新容量 > 旧容量的 2 倍 → 直接用新容量
// 2. 否则：
//    - 旧容量 < 256 → 新容量 = 旧容量 × 2（翻倍）
//    - 旧容量 >= 256 → 新容量 = 旧容量 + 旧容量/4 + 192（约 1.25 倍增长）
// 3. 最终容量会被内存对齐（向上取整到内存分配器的 size class）

s := make([]int, 0)
// append 时容量变化（实际受内存对齐影响）：
// 0 → 1 → 2 → 4 → 8 → 16 → 32 → 64 → ...
```

## 三、Slice 常见陷阱

### 陷阱 1：共享底层数组

```go
a := []int{1, 2, 3, 4, 5}
b := a[1:3]  // b = [2, 3]，和 a 共享底层数组

b[0] = 99
fmt.Println(a)  // [1, 99, 3, 4, 5] ← a 也变了！

// 解决：用 copy 创建独立副本
b := make([]int, 2)
copy(b, a[1:3])  // b 有自己的底层数组
```

### 陷阱 2：append 可能不修改原 slice

```go
func appendDemo() {
    s := make([]int, 0, 5)
    s = append(s, 1, 2, 3)

    // cap 够用：append 不扩容，底层数组不变
    s2 := append(s, 4)  // s2 和 s 共享底层数组
    s3 := append(s, 5)  // s3 也在同一个底层数组写入位置 [3]
    // s2[3] = 5（被 s3 的 append 覆盖了！）

    // cap 不够：append 扩容，新底层数组
    s4 := append(s, 6, 7, 8, 9)  // 超过 cap=5，扩容
    // s4 和 s 不再共享
}
```

### 陷阱 3：nil slice vs 空 slice

```go
var s1 []int         // nil slice
s2 := []int{}        // 空 slice
s3 := make([]int, 0) // 空 slice

// 功能上几乎一样
len(s1) == 0  // true
len(s2) == 0  // true
s1 = append(s1, 1)  // ✅ 正常工作

// 区别：
s1 == nil  // true
s2 == nil  // false

// JSON 编码不同：
json.Marshal(s1)  // "null"
json.Marshal(s2)  // "[]"

// 建议：API 返回空列表时用 make([]T, 0) 或 []T{}，避免返回 null
```

### 陷阱 4：for range 的值拷贝

```go
type Item struct{ Value int }

items := []Item{{1}, {2}, {3}}
for _, item := range items {
    item.Value = 100  // ⚠️ 修改的是副本！原 slice 不变
}
// items = [{1}, {2}, {3}]（没变）

// 修复方式一：用索引
for i := range items {
    items[i].Value = 100  // ✅
}

// 修复方式二：用指针 slice
ptrs := []*Item{{1}, {2}, {3}}
for _, p := range ptrs {
    p.Value = 100  // ✅ 修改原对象
}
```

### 陷阱 5：大 slice 导致内存泄漏

```go
// 问题：小 slice 引用大底层数组，GC 无法回收
func getHeader(data []byte) []byte {
    return data[:10]  // 只要前 10 字节，但底层数组整个被引用
    // 即使 data 已无引用，底层数组仍不会被 GC
}

// 修复：copy 到新 slice
func getHeader(data []byte) []byte {
    header := make([]byte, 10)
    copy(header, data[:10])
    return header  // header 有独立底层数组，data 可被 GC
}
```

## 四、Map 底层结构

```go
// runtime/map.go
type hmap struct {
    count     int            // 键值对数量（len）
    flags     uint8          // 标志位（是否正在写）
    B         uint8          // 桶的对数（桶数量 = 2^B）
    noverflow uint16         // 溢出桶计数
    hash0     uint32         // 哈希种子（随机化，防 Hash DoS）
    buckets   unsafe.Pointer // 桶数组（*bmap）
    oldbuckets unsafe.Pointer // 旧桶（扩容时使用）
    nevacuate  uintptr        // 搬迁进度
    extra     *mapextra       // 溢出桶链表
}

// 每个桶（bmap）存 8 个 key-value 对
type bmap struct {
    tophash [8]uint8  // 8 个 key 的哈希高 8 位（快速比较）
    // 后面紧跟 8 个 key 和 8 个 value（编译器生成，非显式字段）
    // overflow *bmap  // 溢出桶指针
}
```

```
定位 key 流程：
  hash(key) → 取低 B 位 → 桶编号 → 取高 8 位 → 在桶内比较 tophash → 命中后比较完整 key
```

## 五、Map 扩容

```go
// 触发条件：
// 1. 负载因子 > 6.5（count / 2^B > 6.5）→ 翻倍扩容
// 2. 溢出桶过多 → 等量扩容（整理碎片）

// 扩容方式：渐进式搬迁（类似 Redis rehash）
// 不是一次性搬完，每次读写时搬一两个桶，避免大停顿
```

## 六、Map 常见陷阱

### 陷阱 1：并发读写 panic

```go
m := make(map[string]int)

// ❌ 并发写 → fatal error: concurrent map writes
go func() { m["a"] = 1 }()
go func() { m["b"] = 2 }()

// ❌ 并发读写 → fatal error: concurrent map read and map write
go func() { m["a"] = 1 }()
go func() { _ = m["a"] }()

// 解决方案：
// 1. sync.RWMutex
// 2. sync.Map（读多写少场景）
// 3. 分片 map（高并发场景）
```

#### 方案一：sync.RWMutex

```go
type SafeMap struct {
    mu sync.RWMutex
    m  map[string]int
}

func NewSafeMap() *SafeMap {
    return &SafeMap{m: make(map[string]int)}
}

func (s *SafeMap) Set(key string, val int) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.m[key] = val
}

func (s *SafeMap) Get(key string) (int, bool) {
    s.mu.RLock()      // 读锁：多个 goroutine 可同时读
    defer s.mu.RUnlock()
    v, ok := s.m[key]
    return v, ok
}

func (s *SafeMap) Delete(key string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    delete(s.m, key)
}

// 使用
sm := NewSafeMap()
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func(i int) {
        defer wg.Done()
        sm.Set(fmt.Sprintf("key%d", i), i)
    }(i)
}
wg.Wait()
```

特点：读多写少时性能好（RLock 不互斥），写少量时开销低。

---

#### 方案二：sync.Map

```go
var m sync.Map

// 写入
m.Store("name", "jake")
m.Store("age", 28)

// 读取
if val, ok := m.Load("name"); ok {
    fmt.Println(val.(string))  // 注意：返回 any，需要断言
}

// 不存在则写入（原子操作）
actual, loaded := m.LoadOrStore("name", "new_value")
fmt.Println(loaded)  // true → 已存在，actual = "jake"

// 删除
m.Delete("age")

// 遍历（遍历期间可以并发写，但不保证遍历到新写入的 key）
m.Range(func(k, v any) bool {
    fmt.Println(k, v)
    return true  // 返回 false 则停止遍历
})

// ⚠️ 注意：sync.Map 的局限性
// 1. 无法直接知道 map 的大小（没有 Len 方法）
// 2. value 是 any，读取需要类型断言，有运行时开销
// 3. 写多读少时性能比 RWMutex 差（内部有 dirty map 促进机制）
```

内部原理：
```
sync.Map 内部结构：
  read map（atomic.Value）← 无锁读，大多数读操作走这里
  dirty map（mutex 保护）← 写操作和读 miss 走这里
  misses 计数器 → misses 过多时，dirty 提升为 read
```

适合场景：key 基本固定（只写一次，读多次），或各 goroutine 读写不同的 key（分散冲突）。

---

#### 方案三：分片 Map（高并发读写）

```go
const shardCount = 32  // 分片数，通常取 2 的幂

type ShardedMap struct {
    shards [shardCount]*Shard
}

type Shard struct {
    mu sync.RWMutex
    m  map[string]int
}

func NewShardedMap() *ShardedMap {
    sm := &ShardedMap{}
    for i := range sm.shards {
        sm.shards[i] = &Shard{m: make(map[string]int)}
    }
    return sm
}

// fnv hash 确定分片
func (sm *ShardedMap) getShard(key string) *Shard {
    h := fnv.New32a()
    h.Write([]byte(key))
    return sm.shards[h.Sum32()%shardCount]
}

func (sm *ShardedMap) Set(key string, val int) {
    shard := sm.getShard(key)
    shard.mu.Lock()
    defer shard.mu.Unlock()
    shard.m[key] = val
}

func (sm *ShardedMap) Get(key string) (int, bool) {
    shard := sm.getShard(key)
    shard.mu.RLock()
    defer shard.mu.RUnlock()
    v, ok := shard.m[key]
    return v, ok
}

// 使用
sm := NewShardedMap()
var wg sync.WaitGroup
for i := 0; i < 10000; i++ {
    wg.Add(1)
    go func(i int) {
        defer wg.Done()
        sm.Set(fmt.Sprintf("key%d", i), i)
    }(i)
}
wg.Wait()
```

原理：锁竞争分散到 32 个独立的 shard 上，并发冲突概率降到 1/32。
```
key → hash → shard index
              ┌─────────┐
key1 → 0  →  │ shard[0] │ ← 独立锁
key2 → 5  →  │ shard[5] │ ← 独立锁
key3 → 0  →  │ shard[0] │ ← 同一把锁才竞争
              └─────────┘
```

---

#### 三种方案对比

| 方案 | 适用场景 | 优点 | 缺点 |
|------|---------|------|------|
| `sync.RWMutex` | 通用，读多写少 | 简单，性能稳定 | 全局一把锁，高并发写有瓶颈 |
| `sync.Map` | key 固定，读多写极少 | 读无锁，内置原子操作 | 写多时性能差，无 Len，类型不安全 |
| 分片 Map | 高并发读写 | 锁粒度细，吞吐最高 | 实现复杂，遍历需聚合各分片 |

---

### 陷阱 2：遍历顺序随机

```go
m := map[string]int{"a": 1, "b": 2, "c": 3}
for k, v := range m {
    fmt.Println(k, v)  // 每次运行顺序可能不同！
}
// Go 故意随机化遍历顺序，防止代码依赖顺序
```

### 陷阱 3：不能对 value 取地址

```go
m := map[string]User{"jake": {Name: "Jake", Age: 28}}
// m["jake"].Age = 29  // ❌ 编译错误：cannot assign to struct field in map

// 原因：map 扩容可能导致 value 地址变化
// 解决：value 用指针
m2 := map[string]*User{"jake": {Name: "Jake", Age: 28}}
m2["jake"].Age = 29  // ✅
```

### 陷阱 4：nil map 读安全，写 panic

```go
var m map[string]int  // nil
_ = m["key"]          // ✅ 返回零值 0
m["key"] = 1          // ❌ panic: assignment to entry in nil map

// 必须初始化后才能写入
m = make(map[string]int)
m["key"] = 1  // ✅
```

### 陷阱 5：delete 后内存不释放

```go
m := make(map[int]int)
for i := 0; i < 1000000; i++ {
    m[i] = i
}
for i := 0; i < 1000000; i++ {
    delete(m, i)
}
// 此时 m 的底层内存仍被占用（桶数组不会缩小）
// 解决：如果需要释放内存，设 m = nil 或重新 make
```

## 七、make 参数选择

```go
// 知道容量时务必预分配（避免多次扩容）

// ✅ 好：预分配
s := make([]int, 0, 100)  // len=0, cap=100
for i := 0; i < 100; i++ {
    s = append(s, i)  // 不会扩容
}

// ❌ 差：零容量
var s []int
for i := 0; i < 100; i++ {
    s = append(s, i)  // 扩容多次：1→2→4→8→16→32→64→128
}

// Map 同理
m := make(map[string]int, 100)  // 预分配 100 个位置
```

---

## 八、面试高频题

### Q1: slice 底层结构？append 后地址可能变吗？

底层是 {指针, len, cap}。append 如果 cap 不够会重新分配底层数组（地址变化），原 slice 和新 slice 不再共享。

### Q2: 为什么 map 并发不安全？如何检测？

hmap 没有内置锁。运行时通过 flags 字段检测并发写，直接 fatal（不是 panic，不可 recover）。用 `go run -race` 检测。

### Q3: map 遍历为什么是随机的？

Go 故意在 range map 时随机化起始桶位置，防止开发者依赖遍历顺序（遍历顺序本来就不是 spec 保证的）。

### Q4: slice 扩容规则？

Go 1.18+：cap < 256 翻倍；cap >= 256 按约 1.25 倍增长（加平滑因子）。最终按内存分配器 size class 对齐。

### Q5: 如何安全地并发使用 map？

三种方案：1. `sync.RWMutex` 保护 map 2. `sync.Map`（读多写少） 3. 分片锁 map（高并发读写）。
