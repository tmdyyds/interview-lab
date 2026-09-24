# 面试题记录

---

## Q1: 前端一个翻页列表，每页要同时展示两个表的混合数据，并且要求按照两个表混合后的时间排序输出，如何设计？

假设两个表是 `posts`（帖子）和 `comments`（评论），要按 `created_at` DESC 合并分页。

**方案一：UNION ALL（同库，最简单）**

```sql
SELECT id, 'post' AS type, title AS content, created_at
FROM posts
WHERE user_id = ?

UNION ALL

SELECT id, 'comment' AS type, content, created_at
FROM comments
WHERE user_id = ?

ORDER BY created_at DESC, type ASC, id DESC   -- ★ 加 type 和 id 做 tie-breaker，避免同秒记录顺序漂移
LIMIT 20 OFFSET 100;
```

两表都要建 `(user_id, created_at DESC)` 索引。适合数据量不大 + 不深分页的场景。

**方案二：游标分页 + 应用层归并**

不用 offset，用上一页最后一条作为游标，每表各查 `limit` 条后归并。

**⚠️ 坑**：cursor 不能只用 `(created_at, id)`——两个表的 id 各自独立自增，跨表比较 id 没意义。

反例：
```
第一页最后一条 c8 = (created_at=88, comments.id=5)
posts 表有 p13 = (created_at=88, posts.id=13)   ← 时间相同，应出现在第二页

查询 posts：WHERE (created_at, id) < (88, 5)
→ (88, 13) < (88, 5) 为 false → p13 被漏
```

**修正做法（三选一）**：

**A. cursor 用全局单调递增 ID（雪花算法）**——最稳。改表结构，用 snowflake 替代自增 id。所有跨表比较基于同一 ID 空间，无 tie-break 问题。

```go
// cursor = snowflakeID（本身含时间信息）
db.Where("user_id = ? AND snowflake_id < ?", uid, cursor).
   Order("snowflake_id DESC").Limit(20).Find(&posts)
// comments 同理，用同一个 cursor 值
```

**B. cursor 用 (created_at, type, id)，同秒记录约定固定类型顺序**

**约定**：同秒时按 type 字典序，`comment` 在前、`post` 在后（因为 `'c' < 'p'`，type ASC 自然如此）。

**cursor 结构**：`{ CreatedAt, Type: "comment" | "post", ID }`；第一页 cursor = null。

**完整 SQL（参数化）**

posts 表：

```sql
SELECT id, 'post' AS type, title AS content, created_at
FROM posts
WHERE user_id = :uid
  AND (
        :cursor_time IS NULL                                                       -- 第一页
     OR created_at < :cursor_time                                                  -- 更老的秒
     OR (created_at = :cursor_time AND :cursor_type = 'comment')                   -- cursor 停在 comment 段 → posts 同秒都还没读，全要
     OR (created_at = :cursor_time AND :cursor_type = 'post' AND id < :cursor_id)  -- cursor 停在 post 段 → 同段 id 更小的
  )
ORDER BY created_at DESC, id DESC
LIMIT 20;
```

comments 表：

```sql
SELECT id, 'comment' AS type, content, created_at
FROM comments
WHERE user_id = :uid
  AND (
        :cursor_time IS NULL                                                          -- 第一页
     OR created_at < :cursor_time                                                     -- 更老的秒
     OR (created_at = :cursor_time AND :cursor_type = 'comment' AND id < :cursor_id)  -- cursor 停在 comment 段 → 同段 id 更小的
     -- 注意：cursor 停在 post 段时，同秒 comment 段早已在上一页读完，不用取（不需要额外分支）
  )
ORDER BY created_at DESC, id DESC
LIMIT 20;
```

**对应 Go 代码**

```go
type Cursor struct {
    CreatedAt time.Time
    Type      string   // "comment" or "post"
    ID        int64
}

func FetchPage(ctx context.Context, uid int64, cursor *Cursor, limit int) ([]Item, *Cursor, error) {
    // 构造 posts 查询
    postsQ := db.WithContext(ctx).
        Model(&Post{}).
        Where("user_id = ?", uid).
        Order("created_at DESC, id DESC").
        Limit(limit)

    // 构造 comments 查询
    commentsQ := db.WithContext(ctx).
        Model(&Comment{}).
        Where("user_id = ?", uid).
        Order("created_at DESC, id DESC").
        Limit(limit)

    // 加游标条件（第一页 cursor=nil 时跳过）
    if cursor != nil {
        // posts：cursor 停在 comment 段时同秒全要；停在 post 段时同段 id < cursor.id
        if cursor.Type == "comment" {
            postsQ = postsQ.Where(
                "created_at < ? OR created_at = ?",
                cursor.CreatedAt, cursor.CreatedAt,
            )
        } else { // "post"
            postsQ = postsQ.Where(
                "created_at < ? OR (created_at = ? AND id < ?)",
                cursor.CreatedAt, cursor.CreatedAt, cursor.ID,
            )
        }

        // comments：cursor 停在 comment 段时同段 id < cursor.id；停在 post 段时只要更老的
        if cursor.Type == "comment" {
            commentsQ = commentsQ.Where(
                "created_at < ? OR (created_at = ? AND id < ?)",
                cursor.CreatedAt, cursor.CreatedAt, cursor.ID,
            )
        } else { // "post"
            commentsQ = commentsQ.Where("created_at < ?", cursor.CreatedAt)
        }
    }

    // 并发查两表
    var posts []Post
    var comments []Comment
    var wg sync.WaitGroup
    var err1, err2 error
    wg.Add(2)
    go func() { defer wg.Done(); err1 = postsQ.Find(&posts).Error }()
    go func() { defer wg.Done(); err2 = commentsQ.Find(&comments).Error }()
    wg.Wait()
    if err1 != nil { return nil, nil, err1 }
    if err2 != nil { return nil, nil, err2 }

    // 归并两个已排序数组，按 (time DESC, type ASC, id DESC) 全局规则
    merged := mergeByTimeDesc(posts, comments, limit)

    // 生成下一页 cursor
    var next *Cursor
    if len(merged) == limit {
        last := merged[len(merged)-1]
        next = &Cursor{CreatedAt: last.CreatedAt, Type: last.Type, ID: last.ID}
    }
    return merged, next, nil
}

// 归并：全局排序规则 (time DESC, type ASC, id DESC)
func mergeByTimeDesc(posts []Post, comments []Comment, limit int) []Item {
    out := make([]Item, 0, limit)
    i, j := 0, 0
    for len(out) < limit && (i < len(posts) || j < len(comments)) {
        switch {
        case i >= len(posts):
            out = append(out, Item{Type: "comment", CreatedAt: comments[j].CreatedAt, ID: comments[j].ID /* ... */})
            j++
        case j >= len(comments):
            out = append(out, Item{Type: "post", CreatedAt: posts[i].CreatedAt, ID: posts[i].ID /* ... */})
            i++
        default:
            p, c := posts[i], comments[j]
            if p.CreatedAt.After(c.CreatedAt) {
                out = append(out, Item{Type: "post", CreatedAt: p.CreatedAt, ID: p.ID})
                i++
            } else if p.CreatedAt.Before(c.CreatedAt) {
                out = append(out, Item{Type: "comment", CreatedAt: c.CreatedAt, ID: c.ID})
                j++
            } else {
                // 同秒：comment 优先（type ASC）
                out = append(out, Item{Type: "comment", CreatedAt: c.CreatedAt, ID: c.ID})
                j++
            }
        }
    }
    return out
}
```

**推荐索引**：

```sql
ALTER TABLE posts    ADD INDEX idx_uid_ct_id (user_id, created_at, id);
ALTER TABLE comments ADD INDEX idx_uid_ct_id (user_id, created_at, id);
```

**说明**：SQL 里 `:cursor_type = 'comment'` 这种"字符串常量比较"看着怪，其实在**执行时已经是具体值**，只会命中其中一支分支，优化器会把恒假的支砍掉——不影响索引利用。

**C. 时间戳升到微秒/纳秒精度**——业务上大幅降低同秒冲突概率，然后**接受极小的漏取风险**。适合非严格一致的 Feed 流场景。

**优缺点**：翻多少页都 O(log N + M)，无 offset 放大；缺点是不支持跳页，且 tie-break 必须处理好。

**方案三：反范式索引表（要跳页 + 数据量大）**

建一张统一的 timeline 表，两个业务表写入时通过 binlog CDC（Canal/Debezium）异步同步进来：

```sql
CREATE TABLE user_timeline (
    user_id    BIGINT,
    created_at DATETIME(6),
    ref_type   TINYINT,        -- 1=post 2=comment
    ref_id     BIGINT,
    title      VARCHAR(255),   -- 冗余，避免回表
    PRIMARY KEY (user_id, created_at, ref_type, ref_id)
);

-- 查询变成单表：
SELECT * FROM user_timeline WHERE user_id = ? ORDER BY created_at DESC LIMIT 20 OFFSET 100;
```

代价是秒级延迟 + 存储放大，但支持跳页和大数据量。

**方案四：Redis ZSet（超大规模 Feed 流）**

`ZADD user:timeline:{uid} {timestamp} {type}:{id}`，`ZREVRANGEBYSCORE` 分页。ZSet 只留最新 N 条，冷数据回 DB。

**关键坑**：
- 排序必须加 tie-breaker（同秒记录会漂移）
- 游标必须复合 `(created_at, id)`，只用时间会漏取或重取
- 千万别"每表各取 N/2"（数据分布不均时结果全错）

