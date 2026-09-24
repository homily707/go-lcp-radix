# pkg/tree 基准结果

按实现版本分节记录，后续演进（占用记账、驱逐等）在同一节下追加小节，或为触发基准变化的改动新开一节。方案背景与验收指标见 [README.md](README.md) 第 6.1 节。

所有基准共用 `tree_bench_test.go`，只依赖 `New/Select/Insert` 黑盒 API。

当前基准已改用 `New(10)`，Select 最多返回 10 个候选。下列历史结果测量的是未截断候选的版本，不能直接作为当前 TopK 实现的性能数据。

当前实现已移除 `workloadState`，改为共享 `*Workload`，容量字段为 `Capacity int`。第一节（`workloadState 驻留`）及其内历史小节的数据记录旧实现，不能直接当作当前实现的性能结果；该节末尾「收紧内存布局与恢复路径压缩」（commit `48e658a`）小节是当前最新数据。

轮次规范：2026-09-24 起统一 `-count=3`（benchstat 汇总取中位数；3 轮不足以给出 95% 置信区间，表格省略 ± 列，需要显著性判断时临时加轮）。此前各节沿用记录时的轮次。

## 整树 RWMutex + workloadState 驻留（历史结果）

基于 `edb89bb` 之上的未提交改动，距上一节的行为差异：

- values 由 `[]Workload` 改为 `[]residency`（`*workloadState` 指针比较取代含 3 个 string 的结构体内容比较），`workloads` 映射规范化身份；
- workload 身份改为只按 `Workload.ID` 匹配：映射键从结构体变为单个 string，intern 命中已有 ID 时在写锁下原地刷新 Group/MaxCachedTokens，支持动态更新；
- Select 重写：去掉 `seen` map，路径栈上分配（`pathStackDepth` 内零堆分配），相邻层差集去重，结果一次分配到位；
- Insert 对"输入在 segment 内部结束且子节点已含该 workload"的已覆盖短前缀免分裂；
- 并行基准改为每 goroutine 局部 sink（修复 benchSink 数据竞争，消除与树无关的共享写争用）。

- 机器：Apple M2 Pro（10 核可用，GOMAXPROCS=10）
- Go：go1.26.0 darwin/arm64
- 命令：`go test -run='^$' -bench=. -benchmem -count=3 ./pkg/tree/`
- 统计：benchstat，3 轮中位数
- 日期：2026-09-24

### 耗时

| 基准 | ns/op |
| --- | --- |
| Select s=100/p=8/t=32 full | 952.2n |
| Select s=100/p=8/t=32 partial | 1.006µ |
| Select s=100/p=8/t=32 deep-miss | 1.001µ |
| Select s=100/p=8/t=32 miss | 23.26n |
| Select s=1000/p=8/t=32 full | 10.37µ |
| Select s=1000/p=8/t=32 partial | 10.22µ |
| Select s=1000/p=8/t=32 deep-miss | 10.07µ |
| Select s=1000/p=8/t=32 miss | 22.89n |
| Select s=4000/p=16/t=64 full | 27.33µ |
| Select s=4000/p=16/t=64 partial | 27.40µ |
| Select s=4000/p=16/t=64 deep-miss | 27.59µ |
| Select s=4000/p=16/t=64 miss | 23.07n |
| SelectParallel（s=1000） | 11.81µ |
| InsertNewSessions s=100 | 34.34µ |
| InsertNewSessions s=1000 | 564.8µ |
| InsertNewSessions s=4000 | 3.939m |
| InsertReRegister | 73.22n |
| InsertAppend roundChunks=1 | 145.0n |
| InsertAppend roundChunks=4 | 416.9n |
| InsertSplit | 79.07µ |
| MixedSelectInsert | 9.792µ |

### 内存与分配

| 基准 | B/op | allocs/op |
| --- | --- | --- |
| Select s=100 full/partial/deep-miss | 6.000Ki | 1 |
| Select s=100 miss | 0 | 0 |
| Select s=1000 full/partial/deep-miss | 56.00Ki | 1 |
| Select s=1000 miss | 0 | 0 |
| Select s=4000 full/partial/deep-miss | 224.0Ki | 1 |
| Select s=4000 miss | 0 | 0 |
| SelectParallel | 56.02Ki | 1 |
| InsertNewSessions s=100 | 86.35Ki | 435 |
| InsertNewSessions s=1000 | 979.2Ki | 4060 |
| InsertNewSessions s=4000 | 5.809Mi | 16120 |
| InsertReRegister | 0 | 0 |
| InsertAppend roundChunks=1 | 81 | 0 |
| InsertAppend roundChunks=4 | 363 | 1 |
| InsertSplit | 96.93Ki | 880 |
| MixedSelectInsert | 49.02Ki | 0 |

### 观察

- Select 单次堆分配：栈上 path + 相邻层差集 + 预容量结果切片，31→1 allocs；ns/op 随候选集降低约一个数量级（s=1000 full 111µ→10.4µ）。B/op 同比下降（消失的 seen map 与深路径的堆分配，不再是每层累积结构）。
- InsertReRegister 1.78µ→73n：register 深处命中即停 + 指针比较取代 string 内容比较，且 values 结构体更小。
- InsertSplit 284µ→79µ、InsertNewSessions s=4000 26.8m→3.9m：去重与克隆都在指针级，分裂复制的 residency 只有 8 字节。
- MixedSelectInsert 70.2µ→9.8µ 且 0 allocs：读路径免分配后写锁阻塞的实际成本大幅缩小；局部 sink 消除了基准自身的共享写争用，此数字才首次可信（修复前该基准带 -race 直接失败）。
- InsertAppend roundChunks=1 保持 145n/0 allocs，与上一节一致：合并追加路径未被本轮改动触及。
- miss 路径 23n（旧节 26.5n，每节点原子发布基线为 13n）：RWMutex 读锁的固定成本，结论不变。

### 局限

- 3 轮无置信区间；关键行 3 轮极差多在 ±5% 内（MixedSelectInsert 约 ±8%）。
- 单机 10 核笔记本，绝对值仅作参考。
- 未覆盖 workload 切换与驱逐（尚未实现）。
- 未测量锁持有时间分布与 Select 锁等待 p99（§6.1 指定指标，待驱逐实现后补）。

### workload 身份改为按 ID 匹配（2026-09-24，未提交）

在上一小节同一批未提交改动之上，`workloads` 映射键由三字段结构体改为 `Workload.ID` 单个 string；intern 命中已有 ID 时在写锁下原地刷新 Group/MaxCachedTokens。本小节数字是首批按 `New(10)` 截断候选测量的结果，Select/Mixed 各行与本节上方历史表的差异主要来自 TopK 截断（见文首说明），不能直接归因于 ID 键。

- 命令：`go test -run='^$' -bench=. -benchmem -count=3 ./pkg/tree/`
- 统计：benchstat，3 轮中位数

| 基准 | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| Select s=100 full/partial/deep-miss | 381–427n | 904 | 4 |
| Select s=100 miss | 24.58n | 0 | 0 |
| Select s=1000 full/partial/deep-miss | 413–429n | 904 | 4 |
| Select s=1000 miss | 24.23n | 0 | 0 |
| Select s=4000 full/partial/deep-miss | 387–396n | 904 | 4 |
| Select s=4000 miss | 23.83n | 0 | 0 |
| SelectParallel（s=1000） | 303.4n | 904 | 4 |
| InsertNewSessions s=100 | 30.59µ | 77.49Ki | 435 |
| InsertNewSessions s=1000 | 522.7µ | 829.9Ki | 4060 |
| InsertNewSessions s=4000 | 3.816m | 5.225Mi | 16120 |
| InsertReRegister | 66.49n | 0 | 0 |
| InsertAppend roundChunks=1 | 137.7n | 81 | 0 |
| InsertAppend roundChunks=4 | 412.0n | 363 | 1 |
| InsertSplit | 71.98µ | 88.33Ki | 880 |
| MixedSelectInsert | 594.7n | 792 | 3 |

观察：

- Insert 各行相对上一小节（564.8µ/73.22n/79.07µ/3.939m）普遍下降 3–8%，allocs/op 不变、B/op 下降（s=1000 979.2Ki→829.9Ki），与 map 键从 48B 结构体缩为 16B string header 的方向一致。
- InsertReRegister 走 intern 命中 + `ws.key` 原地刷新，仍 0 allocs，66n 略优于 73n。
- Select 恒定 904B/4 allocs：topK=10 下结果切片 + 局部 seen 的固定成本；截断使 s=1000/4000 的 Select 降到 ~400n 量级，候选集不再随会话数增长。

### 收紧内存布局与恢复路径压缩（2026-09-24，commit 48e658a）

上一小节的未提交改动经 `92adeb6`、`8c516a4` 提交后，`48e658a` 继续收紧内存布局：Select 去重由上一小节的局部 seen map 改为相邻层差集（不建集合）；分裂时前缀 segment 克隆为独立数组，不再与后缀共享底层数组；register 后把与唯一子节点驻留集合相同的节点合并，维持路径压缩；边只存子节点首 Chunk 的 Hash（uint64），查找无需解引用 child。数字对比基准为上一小节（未提交改动 + ID 键，`New(10)`）。

- 机器：Apple M2 Pro（10 核可用，GOMAXPROCS=10）
- Go：go1.26.0 darwin/arm64
- 命令：`go test -run='^$' -bench=. -benchmem -count=3 ./pkg/tree/`
- 统计：benchstat，3 轮中位数；无置信区间（样本不足 6）
- 日期：2026-09-24，工作区在 `48e658a` 上干净

#### 耗时

| 基准 | ns/op |
| --- | --- |
| Select s=100 full | 144.5n |
| Select s=100 partial | 134.6n |
| Select s=100 deep-miss | 143.3n |
| Select s=100 miss | 22.91n |
| Select s=1000 full | 164.5n |
| Select s=1000 partial | 153.7n |
| Select s=1000 deep-miss | 169.2n |
| Select s=1000 miss | 22.79n |
| Select s=4000 full | 171.4n |
| Select s=4000 partial | 150.3n |
| Select s=4000 deep-miss | 180.4n |
| Select s=4000 miss | 23.10n |
| SelectParallel（s=1000） | 205.6n |
| InsertNewSessions s=100 | 29.98µ |
| InsertNewSessions s=1000 | 493.2µ |
| InsertNewSessions s=4000 | 3.787m |
| InsertReRegister | 90.93n |
| InsertAppend roundChunks=1 | 153.2n |
| InsertAppend roundChunks=4 | 429.9n |
| InsertSplit | 72.46µ |
| MixedSelectInsert | 302.4n |
| geomean | 518.2n |

#### 内存与分配

| 基准 | B/op | allocs/op |
| --- | --- | --- |
| Select 各 s 的 full/partial/deep-miss | 480 | 1 |
| Select 各 s 的 miss | 0 | 0 |
| SelectParallel | 480 | 1 |
| InsertNewSessions s=100 | 77.37Ki | 436 |
| InsertNewSessions s=1000 | 829.7Ki | 4061 |
| InsertNewSessions s=4000 | 5.225Mi | 16120 |
| InsertReRegister | 48 | 1 |
| InsertAppend roundChunks=1 | 129 | 1 |
| InsertAppend roundChunks=4 | 411 | 2 |
| InsertSplit | 91.27Ki | 945 |
| MixedSelectInsert | 426 | 1 |

#### 观察

- Select full/partial/deep-miss 降到 135–180n（上一小节 380–430n，约 -60%），且不随会话数增长：相邻层差集去重后每次查询只剩结果切片一次分配（904B/4 → 480B/1）；边存内联 hash 使下行查找少两次解引用，边也从 24B 缩到 16B。
- SelectParallel 303n→206n、MixedSelectInsert 595n→302n，B/op 同步降为 480/1：读路径变便宜的直接受益者。
- Insert 构建成本与上一小节持平（NewSessions 30µ/493µ/3.787m、Split 72µ）：分裂前缀克隆与路径压缩合并的额外复制未显著抬高性能；InsertSplit allocs 880→945、B/op 持平，与克隆前缀的额外小数组一致。
- miss 路径 23n 不变，仍是 RWMutex 读锁的固定成本。
- 已知回归：InsertReRegister 66n/0 allocs → 91n/1 alloc（48B），InsertAppend 同样每 op 多 48B/1 alloc（roundChunks=1 的 81B/0 → 129B/1）。归因于 intern 的 `w := &workload` 使 Insert 的 workload 参数整体逃逸：即使 map 命中、结构零修改，每次 Insert 也在调用点堆分配一个 40B 的 Workload（size class 48B），逃逸分析确认（`moved to heap: workload`）。稳态重复登记与多轮追加是最高频的写入形态，该分配属可优化项，暂如实记录。

#### 局限

- 3 轮无置信区间；关键行 3 轮极差多在 ±5% 内。
- 单机 10 核笔记本，绝对值仅作参考。
- 未覆盖 workload 切换与驱逐（尚未实现），锁持有时间分布与 Select 锁等待 p99 仍待补。
- 本节数据测于 `48e658a` 干净工作区；此后的未提交改动（Insert 沿途刷新已保存 Chunk 的 TokenCount）尚未计入，待定稿后补测或并入下一节。

## 整树 RWMutex + 合并追加（workloadState 引入前）

commit `844e4b7`（tree.go 重写：整树 `sync.RWMutex`、可变节点原地修改、独占叶子合并追加、分裂三下标封顶、children 原地增删边）。

- 机器：Apple M2 Pro（10 核可用，GOMAXPROCS=10）
- Go：go1.26.0 darwin/arm64
- 命令：`go test -run='^$' -bench=. -benchmem -count=6 ./pkg/tree/`
- 统计：benchstat，6 轮
- 日期：2025-09-24

### 耗时

| 基准 | ns/op | ± |
| --- | --- | --- |
| Select s=100/p=8/t=32 full | 9.07µs | 3% |
| Select s=100/p=8/t=32 partial | 9.13µs | 2% |
| Select s=100/p=8/t=32 deep-miss | 9.10µs | 3% |
| Select s=100/p=8/t=32 miss | 26.55ns | 1% |
| Select s=1000/p=8/t=32 full | 111.1µs | 2% |
| Select s=1000/p=8/t=32 partial | 109.5µs | 1% |
| Select s=1000/p=8/t=32 deep-miss | 109.9µs | 1% |
| Select s=1000/p=8/t=32 miss | 26.50ns | 1% |
| Select s=4000/p=16/t=64 full | 388.5µs | 5% |
| Select s=4000/p=16/t=64 partial | 397.0µs | 5% |
| Select s=4000/p=16/t=64 deep-miss | 399.0µs | 2% |
| Select s=4000/p=16/t=64 miss | 26.87ns | 0% |
| SelectParallel（s=1000） | 70.26µs | 0% |
| InsertNewSessions s=100 | 45.06µs | 1% |
| InsertNewSessions s=1000 | 1.972ms | 1% |
| InsertNewSessions s=4000 | 26.77ms | 1% |
| InsertReRegister | 1.777µs | 1% |
| InsertAppend roundChunks=1 | 138.6ns | 15% |
| InsertAppend roundChunks=4 | 418.6ns | 1% |
| InsertSplit | 284.0µs | 4% |
| MixedSelectInsert | 70.24µs | 24% |

### 内存与分配

| 基准 | B/op | allocs/op |
| --- | --- | --- |
| Select s=100 full/partial/deep-miss | 30.96Ki | 17 |
| Select s=100 miss | 0 | 0 |
| Select s=1000 full/partial/deep-miss | 375.1Ki | 31 |
| Select s=1000 miss | 0 | 0 |
| Select s=4000 full/partial/deep-miss | 1.726Mi | 61 |
| Select s=4000 miss | 0 | 0 |
| SelectParallel | 375.3Ki | 31 |
| InsertNewSessions s=100 | 83.18Ki | 325 |
| InsertNewSessions s=1000 | 817.9Ki | 3039 |
| InsertNewSessions s=4000 | 5.33Mi | 12070 |
| InsertReRegister | 3 | 0 |
| InsertAppend roundChunks=1 | 81 | 0 |
| InsertAppend roundChunks=4 | 363 | 1 |
| InsertSplit | 309.4Ki | 768 |
| MixedSelectInsert | 328.2Ki | 27 |

### 观察

- miss 路径 26.5ns：RLock/RUnlock 加根即停的全部成本；full/partial/deep-miss 与旧实现无显著差异，锁成本随深度稀释。
- InsertAppend roundChunks=1 零分配：多轮追加合并为独占叶子的原地 append。
- InsertNewSessions s=4000 一轮 5.33Mi：children 原地增删边，不再复制容器。
- MixedSelectInsert 方差 ±24%，读写互相阻塞的信号，需锁等待指标确认。
- Select 的 B/op（s=4000 约 1.7Mi）主要是 path 记录与结果构造，属接口固有成本（README §4.2）。

### 局限

- 单机 10 核笔记本，绝对值仅作参考。
- 未覆盖 workload 切换与驱逐（尚未实现）。
- 未测量锁持有时间分布与 Select 锁等待 p99（§6.1 指定指标，待驱逐实现后补）。

## 每节点原子发布 + 写端 Mutex（旧实现，基线）

commit `a36daf4`（nodeState + atomic.Pointer 不可变发布，每轮追加新建节点，children 修改复制容器）。作为对照基线保留。

- 机器、Go、命令与当前实现一节相同；同机串行运行
- 复现：`git worktree add /tmp/radix-old a36daf4`，拷入当前 `tree_bench_test.go` 后运行同一命令
- 日期：2025-09-24

### 耗时

| 基准 | ns/op | ± |
| --- | --- | --- |
| Select s=100/p=8/t=32 full | 9.08µs | 3% |
| Select s=100/p=8/t=32 partial | 9.02µs | 1% |
| Select s=100/p=8/t=32 deep-miss | 9.43µs | 3% |
| Select s=100/p=8/t=32 miss | 12.96ns | 0% |
| Select s=1000/p=8/t=32 full | 112.2µs | 1% |
| Select s=1000/p=8/t=32 partial | 111.8µs | 11% |
| Select s=1000/p=8/t=32 deep-miss | 113.4µs | 16% |
| Select s=1000/p=8/t=32 miss | 13.55ns | 2% |
| Select s=4000/p=16/t=64 full | 400.9µs | 4% |
| Select s=4000/p=16/t=64 partial | 405.7µs | 2% |
| Select s=4000/p=16/t=64 deep-miss | 400.0µs | 3% |
| Select s=4000/p=16/t=64 miss | 13.71ns | 2% |
| SelectParallel（s=1000） | 70.82µs | 2% |
| InsertNewSessions s=100 | 184.2µs | 1% |
| InsertNewSessions s=1000 | 16.07ms | 3% |
| InsertNewSessions s=4000 | 209.5ms | 7% |
| InsertReRegister | 1.794µs | 1% |
| InsertAppend roundChunks=1 | 1332.5ns | 3% |
| InsertAppend roundChunks=4 | 1524.5ns | 3% |
| InsertSplit | 758.5µs | 8% |
| MixedSelectInsert | 65.59µs | 18% |

### 内存与分配

| 基准 | B/op | allocs/op |
| --- | --- | --- |
| Select s=100 full/partial/deep-miss | 30.96Ki | 17 |
| Select s=100 miss | 0 | 0 |
| Select s=1000 full/partial/deep-miss | 375.1Ki | 31 |
| Select s=1000 miss | 0 | 0 |
| Select s=4000 full/partial/deep-miss | 1.726Mi | 61 |
| Select s=4000 miss | 0 | 0 |
| SelectParallel | 377.8Ki | 31 |
| InsertNewSessions s=100 | 484.47Ki | 1088 |
| InsertNewSessions s=1000 | 40.86Mi | 11200 |
| InsertNewSessions s=4000 | 619.26Mi | 63380 |
| InsertReRegister | 66 | 0 |
| InsertAppend roundChunks=1 | 691 | 6 |
| InsertAppend roundChunks=4 | 905.5 | 7 |
| InsertSplit | 2596.2Ki | 4928 |
| MixedSelectInsert | 330.6Ki | 28 |

### 观察

- InsertNewSessions s=4000 一轮 619Mi、63380 次分配：children 容器复制（O(F)）加每轮追加新建节点，是重写动机的主要来源。
- InsertSplit 分配 2596Ki/4928 次：分裂的复制发布路径。
- miss 路径 13ns：无锁读在根即停查询上的全部成本，是与新实现对比中唯一的读侧优势。
