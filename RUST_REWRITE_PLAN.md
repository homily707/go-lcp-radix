# Rust 重写计划：高 churn Prefix Cache Index

## 1. 目标

将当前 Go radix tree 重写为一个面向 LLM prefix affinity / prefix cache routing 场景的 Rust 专用索引。

这个重写不是对现有 Go 实现逐行翻译，而是重新定义数据模型和 API，使其适合以下实际负载：

- `Select(tokens)` 和 `Insert(tokens, value)` 是两次完全独立的调用。
- Select 时 value / endpoint 尚未确定，因此不能把 Select 和 Insert 合并。
- Insert 时不能依赖 Select 返回的 cursor、node pointer、临时状态或隐藏上下文。
- 多轮对话会产生大量长公共前缀，但完全一致的请求较少。
- 直接或间接分支较多，树会持续发生 split。
- 内存容量固定，因此 Insert 和 Evict 都是高 QPS 操作。
- Select 是高频读路径，应尽量做到零分配、少 pointer chasing。
- Evict 不应重新拿完整 tokens 从 root 搜索。
- prefix affinity 只负责给 routing layer 提供候选 endpoint；容量、负载、健康度等最终调度逻辑仍由外层 router 决定。

第一阶段目标不是设计最复杂的并发树，而是先得到语义正确、内存模型清晰、可 benchmark 的基线实现。

---

## 2. 核心 API

建议第一版固定 token 类型和 endpoint 类型，不做过度泛型化：

```rust
pub type Token = u32;
pub type EndpointId = u32;

pub struct PrefixIndex {
    // internal state
}

pub struct Selection {
    pub matched_tokens: usize,
    pub candidates: [Candidate; MAX_CANDIDATES],
    pub candidate_count: u8,
}

pub struct Candidate {
    pub endpoint: EndpointId,
    pub refs: u32,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct EntryHandle {
    slot: u32,
    generation: u32,
}

impl PrefixIndex {
    pub fn select(&self, tokens: &[Token]) -> Selection;

    pub fn insert(
        &mut self,
        tokens: &[Token],
        endpoint: EndpointId,
    ) -> EntryHandle;

    pub fn evict(&mut self, handle: EntryHandle) -> bool;
}
```

关键约束：

1. `select()` 每次从 root 独立搜索。
2. `insert()` 每次也从 root 独立搜索。
3. 不设计 prepare/commit、cursor reuse、node pointer reuse。
4. `select()` 不返回 prefix token slice，只返回 `matched_tokens`。
5. `insert()` 返回稳定 handle。
6. `evict()` 只依赖 handle，不需要原始 tokens。

---

## 3. 数据结构总体设计

采用：

**Compressed Patricia/Radix Trie + Node/Entry 分离 + stable ID arena + adaptive children + prefix candidate aggregate + handle eviction + lazy compaction**

逻辑结构：

```text
PrefixIndex
├── NodeArena
│   ├── Node
│   ├── Node
│   └── ...
├── EntryArena
│   ├── Entry
│   ├── Entry
│   └── ...
└── Token storage
```

Node 只表达 prefix 结构。

Entry 只表达一次实际 cache record / endpoint record。

不要再把：

- prefix 结构
- exact key
- endpoint/value
- subtree representative

混在一个 Node 的 `Val / End` 字段中。

---

## 4. Node / Entry 分离

### Node

建议逻辑字段：

```rust
struct Node {
    parent: Option<NodeId>,

    edge: TokenSpan,

    children: Children,

    live_entries: u32,

    candidates: CandidateSet,

    exact_entry: Option<EntryId>,
}
```

Node 的职责：

- 保存压缩后的 edge。
- 保存 child lookup 结构。
- 保存 parent，供 Evict 向上更新。
- 保存 subtree 中仍存活的 entry 数量。
- 保存当前 prefix 下 endpoint candidate aggregate。
- 可选保存 exact entry。

### Entry

```rust
struct Entry {
    leaf: NodeId,
    endpoint: EndpointId,
}
```

Entry 的职责只包括：

- 指向该请求最终落到的结构节点。
- 保存 endpoint。
- 作为 eviction handle 对应的逻辑对象。

如果未来一个完整 key 允许多个 endpoint / 多条 cache record，再把 `exact_entry` 从单 EntryId 升级为小集合。

第一版优先针对“完全相同请求较少”的实际流量优化。

---

## 5. Patricia / Radix 路径压缩

继续保留 path compression。

例如：

```text
A B C D E F X
A B C D E F Y
A B C D E F Z
```

不应保存 6 个逐 token 节点，而应表达为：

```text
[A B C D E F]
        |
   -------------
   |     |     |
  [X]   [Y]   [Z]
```

这样可以：

- 减少 node 数。
- 减少树深。
- 降低 child lookup 次数。
- 改善 cache locality。
- 更适合长 system prompt / multi-turn conversation 的共享模式。

需要注意：

compressed depth 下降，不代表 token compare 消失。

Select 和 Insert 都仍需要比较 edge 中的 token。

因此 benchmark 中必须单独测：

- tree traversal 成本
- token comparison 成本

---

## 6. Select 设计

Select 是最重要的读路径。

目标：

- 不分配。
- 不构造 matched prefix。
- 不复制 tokens。
- 不遍历整个 subtree。
- 不扫描所有 child 来寻找 endpoint。
- 时间复杂度主要由匹配 token 数和 compressed depth 决定。

基本流程：

```text
root
  |
lookup first token
  |
compare compressed edge
  |
full match -> continue
partial match / miss -> stop
```

只维护：

```text
matched_tokens
current_node
best_candidate_set
```

返回：

```rust
Selection {
    matched_tokens,
    candidates,
    candidate_count,
}
```

不返回内部 NodeId 给调用方，避免 API 和内部存储布局绑定。

---

## 7. Prefix candidate aggregate

当前 Go 版本通过 Node.Val 保存一个“代表 value”。

这个语义不稳定：

- child 插入会改变 parent value。
- remove 时可能从 map 任意选 child value。
- 同一个 prefix 下多个 endpoint 无法表达。
- 结果受插入顺序影响。

Rust 版本改成显式 candidate aggregate。

例如 subtree 中有：

```text
req1 -> endpoint A
req2 -> endpoint A
req3 -> endpoint B
```

该 prefix 的候选可表达为：

```text
A: 2
B: 1
```

Select 返回 longest matched prefix 对应的 top-K endpoint。

最终 router 再结合：

- endpoint load
- capacity
- health
- saturation
- affinity score

完成最终选择。

PrefixIndex 本身不承担完整负载均衡策略。

---

## 8. CandidateSet 第一版策略

候选集合是一个需要重点 benchmark 的位置。

不要一开始就实现复杂近似算法。

第一版建议：

- endpoint 数量较小时，使用紧凑 `Vec<(EndpointId, refcount)>`。
- 插入时 increment。
- eviction 时 decrement。
- Select 时从集合中选 top-K。
- top-K 数量固定，例如 4。

理由：

- routing 集群的 endpoint 数通常远小于请求数。
- 先得到简单、可验证的正确实现。
- 后续再根据 benchmark 决定是否改成：
  - inline small array
  - dense endpoint counters
  - small hash table
  - cached top-K
  - dirty + lazy rebuild

特别注意：

“每个 Node 永久维护精确 top-K”并不是免费优化。

删除 top candidate 时，需要知道新的替代 candidate。

因此第一版优先保存 refcount 集合，Select 时计算 top-K，而不是设计复杂的删除修复逻辑。

---

## 9. Adaptive Children

不要像当前 Go 版本一样每个 node 都创建 HashMap。

建议 Children 至少分两档：

```rust
enum Children {
    None,

    Inline {
        len: u8,
        items: [(Token, NodeId); 4],
    },

    Hash(...),
}
```

第一版不必做三四档 ART。

策略：

- 0 child：None
- 1~4 child：inline array
- >4 child：hash table

原因：

小 fanout 节点非常多时，HashMap 的 bucket、allocator 和 pointer overhead 很浪费。

inline children 能减少：

- heap allocation
- pointer chasing
- node memory footprint

如果实际 benchmark 发现多数结构节点 fanout 很高，可以进一步简化成：

```text
inline small set -> open addressing hash
```

而不是继续加入 sorted vector 等更多层级。

---

## 10. Arena + stable ID

Rust 版本尽量避免：

```rust
Box<Node>
Rc<Node>
Arc<Node>
```

组成大量独立小对象。

建议使用 slot arena：

```text
NodeArena
EntryArena
```

内部引用全部使用整数 ID。

例如：

```rust
struct NodeId {
    slot: u32,
    generation: u32,
}
```

或者内部 NodeId 可只用 slot，外部 EntryHandle 必须带 generation。

目标：

- 控制 allocator churn。
- 提高对象局部性。
- 内存占用更容易估算。
- 删除后 slot 可复用。
- 避免 stale handle 误删新 entry。

EntryHandle 使用：

```text
slot + generation
```

是必须项。

因为高 eviction QPS 下 slot 会快速复用。

---

## 11. Token ownership

不能直接长期引用调用方传入的：

```rust
&[Token]
```

因为调用完成后 slice 生命周期结束。

也不应该每个 Node 单独持有：

```rust
Vec<Token>
```

否则会产生大量小 allocation。

目标结构：

```rust
struct TokenSpan {
    offset: u32,
    len: u32,
}
```

Node 保存 TokenSpan。

实际 token 存在集中式 token storage 中。

但 token arena 的回收比 NodeArena 更复杂。

因此分阶段实现：

### Phase 1

先使用：

```rust
Box<[Token]>
```

或等价 owned compact slice。

先保证语义和 benchmark 基线。

### Phase 2

再根据 profiling 决定是否实现：

- size-class slab
- chunk allocator
- shard-local token arena
- periodic compact/rebuild

不要第一版就实现复杂可变长度 allocator。

---

## 12. Insert

Insert 必须从 root 完整重新搜索。

不依赖之前 Select 的任何状态。

主要情况：

### Case A：没有 child

直接创建新的 compressed leaf。

### Case B：edge 完全匹配

继续向下。

### Case C：edge 部分匹配

把现有 edge split 为：

```text
parent
  |
common-prefix
  |       \
old-tail new-tail
```

如果新 key 正好结束在 common-prefix：

该 common node 成为 exact key node。

插入完成后：

- 创建 Entry。
- leaf / exact node 记录 EntryId。
- 从该 node 沿 parent 链向 root：
  - `live_entries += 1`
  - candidate refcount += 1

这部分成本为：

```text
O(compressed depth)
```

不需要重新比较 tokens。

---

## 13. Evict(handle)

Evict 是此次重写的重要 API 改进。

流程：

```text
EntryHandle
   |
EntryArena lookup
   |
Entry { leaf, endpoint }
   |
remove logical entry
   |
walk parent chain
```

沿 parent 更新：

- live_entries--
- endpoint refcount--

当 node：

```text
live_entries == 0
```

时，可以直接从 parent detach 整个空 subtree。

这样 eviction 不需要：

- 保存完整 token key。
- 从 root 重新 LCP search。
- 重新做 token comparison。

---

## 14. Lazy compaction

高 churn 场景不建议每次 Evict 都立即把：

```text
parent -> single child
```

合并成一个长 edge。

否则容易出现：

```text
insert -> split
evict -> merge
insert -> split
evict -> merge
```

产生持续写放大。

第一版规则：

1. `live_entries == 0` 的 subtree 立即 detach。
2. 仍有 live entry 的单 child intermediate node 不强制立即 merge。
3. 后续可在：
   - 同一路径再次 mutation
   - dead node ratio 超阈值
   - memory pressure
   - explicit maintenance
   时做 opportunistic compaction。

重点是先避免错误和 churn，而不是始终保持理论上最紧凑的树形。

---

## 15. 并发模型

第一版不要直接复刻当前 Go 的 per-node RWMutex。

原因：

- lock protocol 非常复杂。
- node detach 与继续修改 detached subtree 很容易产生逻辑 race。
- 每个 node 携带 lock 会增加 node size。
- 多层 lock hand-over-hand 会增加读路径成本。

第一版建议：

```rust
RwLock<InnerIndex>
```

即 tree/index 级锁。

优点：

- 语义简单。
- 容易保证正确性。
- 可以作为 benchmark 基线。

但要明确：

高 write QPS 时全局 RwLock 很可能最终成为瓶颈。

后续优化顺序建议：

1. 先 benchmark。
2. 优先按天然 cache domain 分 shard，例如：
   - model
   - tenant
   - namespace
3. 如果单 namespace 仍然热点，再考虑：
   - subtree lock
   - lock striping
   - flat prefix hash index
   - snapshot / copy-on-write 等其他结构

不要按 full key hash shard，因为这会破坏 prefix locality。

也不要默认按 first token shard，因为大量 system prompt 可能拥有相同开头，仍然会形成热点。

---

## 16. Capacity 与 eviction policy 解耦

PrefixIndex 只负责：

```text
prefix -> endpoint affinity information
```

不要让它同时承担完整 cache replacement policy。

如果实际 KV cache / backend 会主动发 eviction event：

```text
Insert -> return EntryHandle
backend/cache manager stores handle
backend evicts item
-> PrefixIndex.evict(handle)
```

这是最干净的模型。

如果未来 PrefixIndex 自己也要控制容量，再单独加入 eviction policy。

此时优先考虑：

- CLOCK
- S3-FIFO

而不是精确 global LRU。

因为精确 LRU 每次 hit 都要修改共享链表，是高 QPS 读路径上的额外写热点。

---

## 17. 第一版明确不做的事情

为了让第一版容易实现和 review，以下内容暂缓：

- 不实现 Select + Insert cursor reuse。
- 不实现 lock-free tree。
- 不实现 per-node mutex。
- 不实现复杂 ART。
- 不实现 SIMD token compare。
- 不实现自定义 variable-size token allocator。
- 不实现近似 heavy-hitter candidate 算法。
- 不实现完整 eviction policy。
- 不试图一次解决所有 endpoint routing 逻辑。

这些项目只能在 benchmark 证明它们有价值后加入。

---

## 18. 建议 crate/module 结构

```text
src/
├── lib.rs
├── index.rs
├── node.rs
├── entry.rs
├── arena.rs
├── children.rs
├── candidates.rs
└── tests/
```

逻辑职责：

### lib.rs

只暴露公共 API。

### index.rs

实现：

- select
- insert
- evict
- split
- cleanup

### node.rs

Node / NodeId。

### entry.rs

Entry / EntryHandle。

### arena.rs

slot allocator + generation。

### children.rs

adaptive child container。

### candidates.rs

endpoint refcount 和 top-K 选择。

---

## 19. 测试计划

测试不能只覆盖普通 exact lookup。

至少包含以下类别。

### Prefix correctness

- exact match
- partial edge match
- no child miss
- key 是已有 key 的 prefix
- 已有 key 是新 key 的 prefix
- deep split
- repeated split
- many siblings

### Entry semantics

- same prefix 不同 endpoint
- exact duplicate
- endpoint refcount increment/decrement
- candidate 在 eviction 后正确消失
- subtree 仍有其他 entry 时不能错误删除 candidate

### Handle correctness

- valid handle eviction
- double eviction
- stale generation handle
- slot reuse 后旧 handle 不能删除新 entry

### Cleanup

- leaf eviction
- empty subtree detach
- intermediate node 保留
- lazy compaction 不影响 select correctness

### Randomized property tests

维护一个简单 reference model。

随机执行：

```text
insert
select
evict
```

验证 Rust index 和 reference model 的：

- matched length
- live entry count
- candidate refcount

一致。

---

## 20. Benchmark 计划

必须在进一步优化前建立 benchmark。

### Select

维度：

- token length: 32 / 512 / 4096
- index size: 1K / 100K / 1M entries
- exact
- partial
- early miss
- late miss
- common long prefix
- high fanout

记录：

- ns/op
- allocations/op
- bytes/op

目标：

Select 热路径 0 allocation。

### Insert

测试：

- leaf append
- prefix split
- deep shared prompt
- high fanout
- duplicate exact key

### Evict

测试：

- leaf
- internal exact entry
- zero-live subtree detach
- churn workload

### Mixed workload

至少：

```text
100% Select

99% Select
1% Insert/Evict

90% Select
10% Insert/Evict

high-churn:
Select ≈ Insert
Evict QPS 接近 Insert QPS
```

### Concurrency

对比：

1. single-thread baseline
2. whole-index RwLock
3. namespace sharding
4. 后续新并发模型

不要只测纯 read，因为实际场景 Insert/Evict 都很多。

---

## 21. 额外需要采集的线上分布

在进一步调 Children、CandidateSet、并发策略前，应先收集：

- request token length histogram
- compressed tree depth
- node direct fanout histogram
- endpoint count per namespace
- candidate count per node
- exact duplicate ratio
- split rate
- insert QPS
- evict QPS
- select QPS
- average entry lifetime
- stale / burst eviction pattern
- common prefix length histogram

这些数据会决定：

- inline children 阈值
- top-K 大小
- 是否值得 dense endpoint counter
- 是否值得 token arena
- 是否需要更细粒度锁

---

## 22. 可选的第二种数据结构：block-prefix hash index

Patricia Trie 应先作为第一版基线。

但 LLM KV cache 经常天然按 block 管理，例如每 16 / 32 / 64 tokens 一个 block。

因此后续应单独 benchmark 一种 flat 方案：

```text
prefix block hash -> candidate aggregate
```

例如对：

```text
block0
block0+block1
block0+block1+block2
...
```

维护 prefix hash entry。

Select：

1. 顺序计算 rolling/block hash。
2. 从最长 block prefix 查找。
3. 必要时验证原 token，避免 hash collision。

优点：

- 少 pointer chasing。
- flat hash table 对并发可能更友好。
- 与 KV block 语义更一致。

缺点：

- prefix 共享表达不如 Patricia 自然。
- hash collision 需要处理。
- exact matched token length 需要额外处理。
- Insert/Evict 会更新多个 prefix block entry。

因此这不是第一版替代品，而是 Patricia 基线建立后的性能对照组。

---

## 23. 实施阶段

### Phase 0：定义语义

完成：

- Token / EndpointId 类型
- Select / Insert / Evict API
- exact duplicate 语义
- candidate refcount 语义
- eviction ownership

完成标准：

所有行为能通过 API contract 描述，不依赖当前 Go 实现的 Val/End 行为。

### Phase 1：单线程正确实现

实现：

- compressed Patricia
- Node / Entry 分离
- basic children
- EntryHandle
- insert/select/evict
- live_entries
- candidate refcount
- lazy cleanup

重点：

正确性优先。

### Phase 2：Arena 与小对象优化

实现：

- NodeArena
- EntryArena
- generation handle
- inline children

测量：

- memory / entry
- allocation count
- cache locality

### Phase 3：并发基线

加入：

```rust
RwLock<InnerIndex>
```

并建立 mixed workload benchmark。

### Phase 4：真实流量 benchmark

根据线上分布生成 workload。

重点观察：

- Select p50/p99
- Insert latency
- Evict latency
- lock contention
- node count
- bytes / entry

### Phase 5：有证据地优化

按 profiling 决定是否加入：

- namespace sharding
- cached top-K
- open addressing child table
- token slab
- vectorized token compare
- opportunistic compaction
- block-prefix hash variant

---

## 24. 第一版验收标准

第一版 Rust rewrite 满足以下条件即可合并：

1. Select 和 Insert 是完全独立调用。
2. Select 不依赖 Insert cursor。
3. Evict 使用 stable handle。
4. Node 与 Entry 分离。
5. prefix match 语义有明确测试。
6. endpoint candidate 使用 refcount，而不是 arbitrary representative value。
7. Select 热路径不构造 prefix Vec。
8. 删除不会通过随机 child 决定 value。
9. stale handle 不会误删复用后的 entry。
10. empty subtree 能被回收。
11. 单 child intermediate node 可暂时保留，不要求每次删除立即 recompress。
12. benchmark 覆盖 select / insert / evict / mixed workload。
13. 并发优化必须以 benchmark 数据为依据。

---

## 25. 推荐实现优先级

如果只按收益 / 实现复杂度排序：

```text
P0
Node / Entry 分离
稳定 EntryHandle
独立 Select / Insert
candidate refcount
正确的 compressed Patricia
lazy subtree cleanup

P1
arena
inline children
zero-allocation Select
benchmark

P2
namespace sharding
candidate top-K cache
token storage 优化

P3
custom hash table
custom token allocator
SIMD
更复杂并发树
block-prefix hash alternative
```

第一版最重要的不是把每个常数都压到最低，而是先建立一个不会被错误 API 和错误语义限制住的结构。

后续所有优化都应建立在真实 fanout、prefix length、endpoint cardinality、insert/evict 比例和 lock contention 数据上。
