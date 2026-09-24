package cacheindex

import (
	"slices"
	"sync"
)

// Chunk 用 Hash 标识一个 message。TokenCount 记录其原始 token 数，
// 仅用于长度记账，不参与前缀匹配。
type Chunk struct {
	Hash       uint64
	TokenCount uint64
}

// Workload 标识路由选中的服务节点。ID 是唯一身份字段：树按 ID 匹配
// 与去重，同 ID 即同一 workload。Group 与 Capacity 是随附属性，
// 不构成身份，随该 ID 最近一次登记刷新；负载等其他可变信息放在索引外部。
type Workload struct {
	ID       string
	Group    string
	Capacity int
}

// Tree 按 workload 索引已缓存的前缀。一把 RWMutex 保护整棵树结构
// （segment、tokens、values、children）、workloads 映射与各 workload
// 的占用记账。驱逐与 Remove 尚未实现。
type Tree struct {
	mu   sync.RWMutex
	root *node
	topK int // 初始化后不变；Select 最多返回的候选数。
	// workloads 按 ID 把每个 workload 规范化为唯一的 *workloadState。
	// 树内部只比较指针；同一 ID 在任意时刻只能对应一个对象，否则
	// Select 会重复输出、记账会分裂。
	workloads map[string]*workloadState
}

// Usage 是一个 workload 当前的驻留占用。
type Usage struct {
	Tokens uint64 // 驻留节点的 token 之和，共享前缀在同一 workload 内只计一次。
	Nodes  int    // 驻留节点数；随分裂与合并变化，只作统计参考。
}

// workloadState 是 workload 的内部状态：Workload 为最近一次登记的配置，
// usage 为增量维护的占用，满足 usage.Tokens == Σ 驻留节点的 tokens、
// usage.Nodes == 驻留节点数。所有字段由 Tree.mu 保护。
type workloadState struct {
	Workload
	usage Usage
}

// node 是可变的 radix 节点，所有字段由 Tree.mu 保护。segment 是一段
// 压缩的 Chunk 路径，tokens 缓存其 TokenCount 之和。values 记录覆盖
// 到本节点及以下的 workload 驻留，且满足 values(child) ⊆ values(parent)。
type node struct {
	segment  []Chunk
	tokens   uint64 // == Σ segment[i].TokenCount
	values   []residency
	children childrenSet
}

// residency 是一个 workload 在一个节点上的驻留记录。引入 LRU 后，
// 该 workload 在其 LRU 中的句柄也挂在这里，使热度刷新、分裂与驱逐
// 无需按节点查表。
type residency struct {
	workload *workloadState
}

// childrenSet 最多只有一种有效表示：少量分叉用有序 edge 切片，
// 多分叉用按 Chunk.Hash 索引的 map。修改在 Tree.mu 下原地完成，
// 超过扇出阈值时从 small 升级为 large。
type childrenSet struct {
	small []edge           // large == nil 时有效
	large map[uint64]*node // 以 Chunk.Hash 为键
}

type edge struct {
	hash  uint64 // 子节点 segment[0].Hash 的内联副本，查找无需解引用 child。
	child *node
}

// Match 记录一个 workload 在本次查询中观察到的最佳前缀匹配。
type Match struct {
	Workload Workload
	Length   uint64 // 完整命中 Chunk 的查询 TokenCount 之和。
}

const (
	// smallFanoutLimit 是 edge 切片与 map 两种表示切换的单一阈值。
	smallFanoutLimit = 8
	// pathStackDepth 是 Select/Insert 路径记录的栈上初始容量，
	// 更深的路径退回堆分配。
	pathStackDepth = 32
)

// New 创建空前缀索引，Select 最多返回 topK 个候选。
// topK <= 0 时 Select 返回空结果，不影响 Insert 登记。
func New(topK int) *Tree {
	return &Tree{root: &node{}, topK: topK, workloads: make(map[string]*workloadState)}
}

// Select 返回最多 topK 个正 token 匹配 workload 的最佳结果，按 Length 降序，
// 同分候选任取，顺序不保证。结果切片归调用方所有。整个查询在读锁下进行，
// 写者持锁时会被阻塞；它不写任何共享状态（包括 workload 状态），
// 不更新缓存热度，也不保证快照与完整的驱逐或 Remove 操作对齐。
func (t *Tree) Select(key []Chunk) []Match {
	if len(key) == 0 || t.topK <= 0 {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	type level struct {
		n      *node
		length uint64
	}
	var stack [pathStackDepth]level
	path := stack[:0]
	current := t.root
	var length uint64
	for offset := 0; offset < len(key); {
		child := current.children.get(key[offset].Hash)
		if child == nil {
			break
		}
		matched := 0
		for matched < len(child.segment) && offset < len(key) && child.segment[matched].Hash == key[offset].Hash {
			length += key[offset].TokenCount
			matched++
			offset++
		}
		path = append(path, level{child, length})
		if matched < len(child.segment) {
			break
		}
		current = child
	}

	// length 沿路径单调不减，零长度层只可能出现在最浅处。
	first := 0
	for first < len(path) && path[first].length == 0 {
		first++
	}
	if first == len(path) {
		return nil
	}

	// 从深到浅首次遇到的 workload 即最佳匹配；候选满额立即返回。
	// 节点保留完整驻留集合，查询仅为最多 topK 个候选分配空间。
	//
	// 去重用相邻层差集，不建 seen 集合：未提前返回时，已输出集合恰为
	// 下一层（更深一层）的 values，且 values(child) ⊆ values(parent)，
	// 故本层新候选即 values(i) \ values(i+1)。能走到第 i 层说明第 i+1 层
	// 未满额，|values(i+1)| < limit，每次包含检查至多 topK 次指针比较。
	limit := min(t.topK, len(path[first].n.values))
	matches := make([]Match, 0, limit)
	var below []residency // 更深一层的 values，即已输出集合。
	for i := len(path) - 1; i >= first; i-- {
		values := path[i].n.values
		if len(values) == len(below) {
			continue // 下一层是本层的子集，等长即没有新候选；below 不变。
		}
		for _, r := range values {
			if containsWorkload(below, r.workload) {
				continue
			}
			matches = append(matches, Match{Workload: r.workload.Workload, Length: path[i].length})
			if len(matches) == limit {
				return matches
			}
		}
		below = values
	}
	return matches
}

// Insert 在写锁下登记一个 workload 的缓存前缀。独占叶子被原地延长，
// 多轮追加合并进同一个 segment，而不是每轮新建节点。Insert 复制所有
// 要保存的内容，调用方随后可以复用输入切片。已保存 Chunk 的 TokenCount
// 以首次登记为准，之后不再刷新：调用方保证同 Hash 的 TokenCount 一致。
//
// 占用记账与结构修改同步完成：分裂/建叶/追加各自维护节点 tokens，
// register 登记时按节点 tokens 计入 w。自动驱逐尚未实现，占用可能超过
// Capacity。
//
// 下行时只定位、不登记；结构修改完成后再沿完整匹配的路径（含被分裂的
// 前缀节点）自底向上补登记 values，遇到已含该 workload 的节点即停止
// （其祖先必然也含）。多轮追加与重复登记因此只检查最深处的小集合。
// 登记后把与唯一子节点驻留集合相同的节点合并，维持路径压缩。
func (t *Tree) Insert(key []Chunk, workload Workload) {
	if len(key) == 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	w := t.intern(workload)
	var stack [pathStackDepth]*node
	path := stack[:0] // 完整匹配的非根节点
	current := t.root
	for offset := 0; offset < len(key); {
		child := current.children.get(key[offset].Hash)
		if child == nil {
			if !t.extendExclusiveLeaf(current, w, key[offset:]) {
				current.children.add(key[offset].Hash, newLeaf(key[offset:], w))
			}
			break
		}

		rest := key[offset:]
		matched := commonPrefixLen(child.segment, rest)
		if matched < len(child.segment) {
			// 输入在 segment 内部结束且 child 已覆盖 w：覆盖关系未变，
			// 直接结束即可，分裂只会新增节点并复制 values。child 含 w
			// 则祖先必然也含，register 随即在最深处停止。
			if matched == len(rest) && containsWorkload(child.values, w) {
				break
			}
			split(child, matched, rest, w)
			path = append(path, child) // 前缀节点由 register 登记 w。
			break
		}

		path = append(path, child)
		offset += matched
		current = child
	}
	register(path, w)
}

// Usage 返回 id 对应 workload 的当前占用；从未登记过时 ok 为 false。
// 在读锁下读取，返回副本。
func (t *Tree) Usage(id string) (usage Usage, ok bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	w, ok := t.workloads[id]
	if !ok {
		return Usage{}, false
	}
	return w.usage, true
}

// intern 返回 ID 对应的唯一 workload 状态，不存在时创建。调用方持写锁。
// Group 与 Capacity 不构成身份：同 ID 再次登记时只刷新配置，占用保留。
// 只能在确认没有任何节点仍引用对象之后从映射中删除（随 Remove 引入），
// 占用归零不代表可删；否则并发 Insert 会创建第二个指针与残留引用共存。
func (t *Tree) intern(workload Workload) *workloadState {
	if w, ok := t.workloads[workload.ID]; ok {
		w.Workload = workload
		return w
	}
	w := &workloadState{Workload: workload}
	t.workloads[workload.ID] = w
	return w
}

// register 沿 path 自底向上把 w 登记到各节点，遇到已含 w 的节点即
// 停止：前缀闭合保证其祖先都已含 w。
//
// 登记会让 path[j] 的驻留集合追平其父 path[j-1]；父节点若只有这一个
// 子节点，两者覆盖边界相同，应合并为一个节点。只有新增了 w 的节点
// 才可能与父节点新近相等（停止处以上未变，按不变式已压缩），因此只
// 检查 path[stop+1:] 与各自父节点这几对。自底向上合并：path[j] 吸收
// 更深节点后，其子节点与它的关系不变，无需回头复查。
func register(path []*node, w *workloadState) {
	stop := len(path) - 1
	for ; stop >= 0; stop-- {
		if !path[stop].addValue(w) {
			break
		}
	}
	for j := len(path) - 1; j > stop && j > 0; j-- {
		mergeOnlyChild(path[j-1], path[j])
	}
}

// mergeOnlyChild 在 child 是 parent 唯一子节点且驻留集合相同时，把
// child 并入 parent：parent 保留父边与 values，追加 child 的 segment
// 并接管其 children；child 的 segment 与 values 随之释放。
// values(child) ⊆ values(parent)，等长即相等。parent 的空闲容量只归
// 它自己所有（分裂前缀为独立克隆，后缀独占原数组尾部，其余来自克隆
// 或自身追加），append 不会覆盖其他节点的数据。
// 驻留集合相同，各驻留者已为两个节点分别计费，Tokens 不变，只少一个节点。
// 引入 LRU 后，child 上 residency 的句柄需在此一并迁移或释放。
func mergeOnlyChild(parent, child *node) {
	if len(parent.values) != len(child.values) || !parent.children.single() {
		return
	}
	parent.segment = append(parent.segment, child.segment...)
	parent.tokens += child.tokens
	parent.children = child.children
	for _, r := range parent.values {
		r.workload.usage.Nodes--
	}
}

// newLeaf 复制 tail 建立只驻留 w 的新叶子，并把它计入 w 的占用。
func newLeaf(tail []Chunk, w *workloadState) *node {
	tokens := sumTokens(tail)
	w.usage.Tokens += tokens
	w.usage.Nodes++
	return &node{segment: slices.Clone(tail), tokens: tokens, values: []residency{{workload: w}}}
}

func sumTokens(chunks []Chunk) uint64 {
	var total uint64
	for _, c := range chunks {
		total += c.TokenCount
	}
	return total
}

// extendExclusiveLeaf 把 tail 追加到仅由 w 独占的叶子 segment 上。
// 这样的节点没有其他需要保留的覆盖边界，多轮追加可以合并成单个
// segment。n 不含 w 时自然不满足独占条件。append 可能写入空闲容量，
// 也可能按几何增长扩容；无论哪种，保存的 chunk 都从输入复制而来。
// 追加部分只计入 w 的 Tokens，节点数不变。
func (t *Tree) extendExclusiveLeaf(n *node, w *workloadState, tail []Chunk) bool {
	if n == t.root || !n.children.empty() {
		return false
	}
	if len(n.values) != 1 || n.values[0].workload != w {
		return false
	}
	tokens := sumTokens(tail)
	n.segment = append(n.segment, tail...)
	n.tokens += tokens
	w.usage.Tokens += tokens
	return true
}

// commonPrefixLen 返回前缀 Hash 相同的连续 Chunk 数量。
func commonPrefixLen(segment, key []Chunk) int {
	matched := 0
	for matched < len(segment) && matched < len(key) && segment[matched].Hash == key[matched].Hash {
		matched++
	}
	return matched
}

// split 在写锁下把 child 从第 matched 个 Chunk 处原地分裂，rest 是从
// child 起始处对齐的剩余输入。原节点保留父边和前缀 segment；新建的
// 后缀节点接管 children 和 values 的副本。
//
// 后缀沿用原底层数组，前缀复制为独立数组，分裂后两者不共享内存：
// 前缀是祖先、通常更长寿，若继续引用原数组，后缀被驱逐后整块数组
// 仍被前缀钉住无法回收；独立后也不存在前缀追加覆盖后缀数据的问题。
// 后缀存活期间，原数组头部 matched 个 Chunk 不可达但仍被占用，随后缀
// 释放一并回收。分裂不改变任何 workload 的覆盖；插入的 w 只写入新
// 叶子，前缀节点及其祖先由调用方的 register 登记。
//
// 前缀与后缀 tokens 之和等于原节点，原驻留者的 Tokens 不变，只各多
// 一个节点。
func split(child *node, matched int, rest []Chunk, w *workloadState) {
	prefixTokens := sumTokens(child.segment[:matched])
	suffix := &node{
		segment:  child.segment[matched:],
		tokens:   child.tokens - prefixTokens,
		values:   slices.Clone(child.values),
		children: child.children,
	}
	for _, r := range suffix.values {
		r.workload.usage.Nodes++
	}
	child.segment = slices.Clone(child.segment[:matched])
	child.tokens = prefixTokens
	child.children = childrenSet{}
	child.children.add(suffix.segment[0].Hash, suffix)
	if matched < len(rest) {
		child.children.add(rest[matched].Hash, newLeaf(rest[matched:], w))
	}
}

// addValue 原地把 w 追加进 values 并按 n.tokens 计入其占用，已存在则
// 返回 false。values 切片从不在节点间共享：分裂时克隆，新叶子单独分配。
func (n *node) addValue(w *workloadState) bool {
	if containsWorkload(n.values, w) {
		return false
	}
	n.values = append(n.values, residency{workload: w})
	w.usage.Tokens += n.tokens
	w.usage.Nodes++
	return true
}

func containsWorkload(values []residency, w *workloadState) bool {
	for _, r := range values {
		if r.workload == w {
			return true
		}
	}
	return false
}

func (children childrenSet) get(hash uint64) *node {
	if children.large != nil {
		return children.large[hash]
	}
	for _, edge := range children.small {
		if edge.hash == hash {
			return edge.child
		}
	}
	return nil
}

func (children childrenSet) empty() bool {
	return children.large == nil && len(children.small) == 0
}

// single 报告是否恰有一个子节点。large 只在超过扇出阈值后启用且
// 目前不会降级，故 large 非空即多于一个。
func (children childrenSet) single() bool {
	return children.large == nil && len(children.small) == 1
}

// add 原地插入或替换 hash 对应的边，超过 small 表示的容量后
// 升级为 map。
func (children *childrenSet) add(hash uint64, child *node) {
	if children.large != nil {
		children.large[hash] = child
		return
	}
	for i := range children.small {
		if children.small[i].hash == hash {
			children.small[i].child = child
			return
		}
	}
	children.small = append(children.small, edge{hash: hash, child: child})
	if len(children.small) > smallFanoutLimit {
		large := make(map[uint64]*node, len(children.small))
		for _, edge := range children.small {
			large[edge.hash] = edge.child
		}
		children.large = large
		children.small = nil
	}
}
