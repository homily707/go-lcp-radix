package cacheindex

import (
	"fmt"
	"math/rand"
	"testing"
)

// countNodes 统计非根节点数，用于验证路径压缩不被重复登记破坏。
func countNodes(n *node) int {
	total := 0
	for _, edge := range n.children.small {
		total += 1 + countNodes(edge.child)
	}
	for _, child := range n.children.large {
		total += 1 + countNodes(child)
	}
	return total
}

// 已覆盖的短前缀重复插入不得制造多余节点：w1 先登记 ABCD，再反复登记
// 更短的已覆盖前缀，节点数应保持不变。
func TestInsertCoveredPrefixDoesNotSplit(t *testing.T) {
	key := []Chunk{chunkOf(0), chunkOf(1), chunkOf(2), chunkOf(3)}
	index := New(10)
	index.Insert(key, Workload{ID: "w1"})
	if got := countNodes(index.root); got != 1 {
		t.Fatalf("nodes after first insert = %d, want 1", got)
	}
	for _, n := range []int{2, 1, 3, 2, 4, 1} {
		index.Insert(key[:n], Workload{ID: "w1"})
		if got := countNodes(index.root); got != 1 {
			t.Fatalf("nodes after re-insert key[:%d] = %d, want 1", n, got)
		}
	}
}

// 覆盖关系确实变化时仍须分裂：新 workload 的短前缀登记要产生边界，
// 且不能让它匹配到更长的未缓存部分。
func TestInsertShortPrefixNewWorkloadStillSplits(t *testing.T) {
	key := []Chunk{chunkOf(0), chunkOf(1), chunkOf(2), chunkOf(3)}
	index := New(10)
	index.Insert(key, Workload{ID: "w1"})
	index.Insert(key[:2], Workload{ID: "w2"})
	if got := countNodes(index.root); got != 2 {
		t.Fatalf("nodes = %d, want 2", got)
	}
	matches := index.Select(key)
	seen := map[string]uint64{}
	for _, m := range matches {
		seen[m.Workload.ID] = m.Length
	}
	if seen["w1"] != 40 || seen["w2"] != 20 {
		t.Fatalf("Select = %v, want w1=40 w2=20", seen)
	}
}

// 覆盖关系重新变得一致时应恢复路径压缩：w2 先登记 AB 使 ABCD 分裂，
// 再登记完整 ABCD 后，AB 与其唯一子节点 CD 驻留集合相同，应合并回
// 单个节点，且合并后的 segment 仍能正确匹配中间位置。
func TestInsertMergesOnlyChildWithEqualValues(t *testing.T) {
	key := []Chunk{chunkOf(0), chunkOf(1), chunkOf(2), chunkOf(3)}
	index := New(10)
	index.Insert(key, Workload{ID: "w1"})
	index.Insert(key[:2], Workload{ID: "w2"})
	index.Insert(key, Workload{ID: "w2"})
	if got := countNodes(index.root); got != 1 {
		t.Fatalf("nodes = %d, want 1", got)
	}
	for n, want := range map[int]uint64{4: 40, 3: 30} {
		seen := map[string]uint64{}
		for _, m := range index.Select(key[:n]) {
			seen[m.Workload.ID] = m.Length
		}
		if seen["w1"] != want || seen["w2"] != want {
			t.Fatalf("Select(key[:%d]) = %v, want w1=w2=%d", n, seen, want)
		}
	}
}

// 分裂出的前缀登记新 workload 后，若与其父节点驻留集合相同且为唯一
// 子节点，也应并入父节点：P 为 {w1,w2}，唯一子节点 QRS 只有 w1；
// w2 登记到 Q 处使 QRS 分裂，前缀 Q 变为 {w1,w2}，应并入 P。
func TestInsertMergesSplitPrefixIntoParent(t *testing.T) {
	p := []Chunk{chunkOf(0), chunkOf(1)}
	full := append(append([]Chunk{}, p...), chunkOf(2), chunkOf(3), chunkOf(4))
	index := New(10)
	index.Insert(full, Workload{ID: "w1"})
	index.Insert(p, Workload{ID: "w2"}) // P{w1,w2} -> QRS{w1}
	if got := countNodes(index.root); got != 2 {
		t.Fatalf("nodes before = %d, want 2", got)
	}
	index.Insert(full[:3], Workload{ID: "w2"}) // PQ{w1,w2} -> RS{w1}
	if got := countNodes(index.root); got != 2 {
		t.Fatalf("nodes after = %d, want 2", got)
	}
	seen := map[string]uint64{}
	for _, m := range index.Select(full) {
		seen[m.Workload.ID] = m.Length
	}
	if seen["w1"] != 50 || seen["w2"] != 30 {
		t.Fatalf("Select = %v, want w1=50 w2=30", seen)
	}
}

// checkInvariants 遍历整树从头重算，并与增量维护的状态比较：
// node.tokens == Σ segment、values 无重复且 values(child) ⊆ values(parent)、
// 不存在可合并的单子链（非根节点只有一个子节点且驻留集合相同），
// 以及每个 workload 的 Usage 与按驻留重算的值一致。
func checkInvariants(index *Tree) error {
	want := map[*workloadState]Usage{}
	var walk func(parent, n *node) error
	walk = func(parent, n *node) error {
		var kids []*node
		for _, e := range n.children.small {
			if e.hash != e.child.segment[0].Hash {
				return fmt.Errorf("edge hash %d != child head %d", e.hash, e.child.segment[0].Hash)
			}
			kids = append(kids, e.child)
		}
		for h, c := range n.children.large {
			if h != c.segment[0].Hash {
				return fmt.Errorf("map key %d != child head %d", h, c.segment[0].Hash)
			}
			kids = append(kids, c)
		}
		if parent != nil {
			if len(n.segment) == 0 {
				return fmt.Errorf("non-root node with empty segment")
			}
			if got := sumTokens(n.segment); got != n.tokens {
				return fmt.Errorf("node tokens = %d, want %d", n.tokens, got)
			}
			seen := map[*workloadState]bool{}
			for _, r := range n.values {
				if seen[r.workload] {
					return fmt.Errorf("duplicate residency %s", r.workload.ID)
				}
				seen[r.workload] = true
				if parent != index.root && !containsWorkload(parent.values, r.workload) {
					return fmt.Errorf("%s resident in child but not parent", r.workload.ID)
				}
				u := want[r.workload]
				u.Tokens += n.tokens
				u.Nodes++
				want[r.workload] = u
			}
			if len(kids) == 1 && len(kids[0].values) == len(n.values) {
				return fmt.Errorf("mergeable single-child chain at %v", n.segment[0])
			}
		}
		for _, c := range kids {
			if err := walk(n, c); err != nil {
				return err
			}
		}
		return nil
	}
	if index.root.tokens != 0 || len(index.root.values) != 0 {
		return fmt.Errorf("root carries tokens or values")
	}
	if err := walk(nil, index.root); err != nil {
		return err
	}
	for id, w := range index.workloads {
		if w.ID != id {
			return fmt.Errorf("workloads[%q].ID = %q", id, w.ID)
		}
		if w.usage != want[w] {
			return fmt.Errorf("usage[%s] = %+v, want %+v", id, w.usage, want[w])
		}
		if u, ok := index.Usage(id); !ok || u != want[w] {
			return fmt.Errorf("Usage(%s) = %+v,%v, want %+v", id, u, ok, want[w])
		}
	}
	return nil
}

// 占用记账的定向用例：共享前缀在同一 workload 内只计一次，分裂、合并、
// 独占追加后 Tokens/Nodes 均与重算一致。
func TestUsageAccounting(t *testing.T) {
	key := []Chunk{chunkOf(0), chunkOf(1), chunkOf(2), chunkOf(3)}
	index := New(10)
	usage := func(id string) Usage {
		u, _ := index.Usage(id)
		return u
	}
	step := func(k []Chunk, id string, w1, w2 Usage) {
		t.Helper()
		index.Insert(k, Workload{ID: id})
		if err := checkInvariants(index); err != nil {
			t.Fatal(err)
		}
		if got := usage("w1"); got != w1 {
			t.Fatalf("w1 = %+v, want %+v", got, w1)
		}
		if got := usage("w2"); got != w2 {
			t.Fatalf("w2 = %+v, want %+v", got, w2)
		}
	}
	step(key[:2], "w1", Usage{20, 1}, Usage{})
	step(key, "w1", Usage{40, 1}, Usage{})          // 独占追加
	step(key[:2], "w1", Usage{40, 1}, Usage{})      // 重复登记已覆盖前缀
	step(key[:2], "w2", Usage{40, 2}, Usage{20, 1}) // 分裂
	step(key, "w2", Usage{40, 1}, Usage{40, 1})     // 登记后合并
	step(append(key[:2:2], chunkOf(9)), "w2", Usage{40, 2}, Usage{50, 3})

	if _, ok := index.Usage("missing"); ok {
		t.Fatal("Usage(missing) ok = true")
	}
	// 配置刷新不影响占用。
	index.Insert(key[:1], Workload{ID: "w1", Group: "g", Capacity: 1})
	if got := usage("w1"); got.Tokens != 40 {
		t.Fatalf("w1 after config refresh = %+v", got)
	}
}

// 随机插入（小 Hash 字母表制造大量共享、分叉与短前缀），每步后与
// 整树重算比较。同 Hash 的 TokenCount 固定，符合调用方契约。
func TestUsageAccountingRandomized(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		index := New(4)
		ids := []string{"a", "b", "c", "d", "e"}
		for step := 0; step < 400; step++ {
			n := 1 + rng.Intn(12)
			key := make([]Chunk, n)
			for i := range key {
				h := uint64(rng.Intn(3))
				key[i] = Chunk{Hash: h, TokenCount: 1 + h}
			}
			index.Insert(key, Workload{ID: ids[rng.Intn(len(ids))]})
			if err := checkInvariants(index); err != nil {
				t.Fatalf("seed %d step %d: %v", seed, step, err)
			}
		}
	}
}

func chunkOf(hash uint64) Chunk {
	return Chunk{Hash: hash, TokenCount: 10}
}
