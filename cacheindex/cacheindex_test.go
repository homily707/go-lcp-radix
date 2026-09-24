package cacheindex_test

import (
	"math/rand"
	"reflect"
	"sync"
	"testing"

	"github.com/homily707/go-lcp-radix/cacheindex"
)

// chunk 构造一个 key 元素：hash 标识 message，tokens 是其原始 token 数。
func chunk(hash, tokens uint64) cacheindex.Chunk {
	return cacheindex.Chunk{Hash: hash, TokenCount: tokens}
}

// wl 构造一个以 ID 区分的 workload 身份。
func wl(id string) cacheindex.Workload {
	return cacheindex.Workload{ID: id}
}

// checkMatches 校验 Select 结果：非零长度、按 Length 降序、每个 workload
// 至多出现一次，且按 ID 汇总后与期望的匹配长度一致。
func checkMatches(t *testing.T, got []cacheindex.Match, want map[string]uint64) {
	t.Helper()
	seen := make(map[string]uint64, len(got))
	for i, match := range got {
		if match.Length == 0 {
			t.Fatalf("Select returned zero-length candidate: %+v", match)
		}
		if i > 0 && got[i-1].Length < match.Length {
			t.Fatalf("Select is not ordered by descending length: %+v", got)
		}
		if _, exists := seen[match.Workload.ID]; exists {
			t.Fatalf("Select returned workload %q more than once: %+v", match.Workload.ID, got)
		}
		seen[match.Workload.ID] = match.Length
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("Select candidates = %v, want %v", seen, want)
	}
}

// TestSelectEmptyAndUnmatched 验证空树、空查询和完全未命中的查询都返回
// 空结果，由调用方回退到普通负载均衡。
func TestSelectEmptyAndUnmatched(t *testing.T) {
	index := cacheindex.New(10000)
	a, b := chunk(1, 100), chunk(2, 20)
	checkMatches(t, index.Select(nil), map[string]uint64{})
	checkMatches(t, index.Select([]cacheindex.Chunk{a}), map[string]uint64{})

	index.Insert([]cacheindex.Chunk{a, b}, wl("w1"))
	checkMatches(t, index.Select(nil), map[string]uint64{})
	checkMatches(t, index.Select([]cacheindex.Chunk{chunk(9, 100)}), map[string]uint64{})
}

// TestSelectPartialSegmentAndChunkIdentity 验证压缩段内部的部分命中：
// Chunk 只按 Hash 匹配，命中长度使用查询的 TokenCount。
func TestSelectPartialSegmentAndChunkIdentity(t *testing.T) {
	index := cacheindex.New(10000)
	a, b, c, d := chunk(1, 100), chunk(2, 20), chunk(3, 30), chunk(4, 50)
	index.Insert([]cacheindex.Chunk{a, b, c, d}, wl("w1"))

	cases := []struct {
		name string
		key  []cacheindex.Chunk
		want uint64
	}{
		{"query ends inside segment", []cacheindex.Chunk{a, b}, 120},
		{"first chunk differs", []cacheindex.Chunk{chunk(9, 100), b}, 0},
		{"middle chunk differs", []cacheindex.Chunk{a, b, chunk(9, 30)}, 120},
		{"query extends segment", []cacheindex.Chunk{a, b, c, d, chunk(5, 10)}, 200},
		{"same hash different token count", []cacheindex.Chunk{a, chunk(2, 21)}, 121},
		{"query token counts determine length", []cacheindex.Chunk{chunk(1, 101), chunk(2, 21), c}, 152},
		{"same token count different hash", []cacheindex.Chunk{a, chunk(9, 20)}, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := map[string]uint64{}
			if tc.want > 0 {
				want["w1"] = tc.want
			}
			checkMatches(t, index.Select(tc.key), want)
		})
	}
}

// TestSelectReturnsBestMatchPerWorkloadAcrossLevels 验证多层驻留时每个
// workload 只返回一次且取其最佳匹配长度，同时保留次优层和全部同分者。
func TestSelectReturnsBestMatchPerWorkloadAcrossLevels(t *testing.T) {
	index := cacheindex.New(10000)
	a, b, c, d, x := chunk(1, 100), chunk(2, 20), chunk(3, 30), chunk(4, 50), chunk(5, 40)
	index.Insert([]cacheindex.Chunk{a, b, c, d}, wl("deep"))
	index.Insert([]cacheindex.Chunk{a, b, c, d}, wl("tied"))
	index.Insert([]cacheindex.Chunk{a, b, c, x}, wl("middle"))
	index.Insert([]cacheindex.Chunk{a, b}, wl("shallow"))
	index.Insert([]cacheindex.Chunk{x}, wl("unmatched"))

	query := []cacheindex.Chunk{a, b, c, d, chunk(6, 10)}
	checkMatches(t, index.Select(query), map[string]uint64{
		"deep": 200, "tied": 200, "middle": 150, "shallow": 120,
	})
	checkMatches(t, index.Select([]cacheindex.Chunk{a, b, x}), map[string]uint64{
		"deep": 120, "tied": 120, "middle": 120, "shallow": 120,
	})
}

// 同一 Hash 即使登记和查询时的 TokenCount 不同，也应沿同一分支匹配。
func TestInsertSameHashDifferentTokenCountSharesPath(t *testing.T) {
	index := cacheindex.New(10000)
	index.Insert([]cacheindex.Chunk{chunk(1, 100), chunk(2, 20)}, wl("w1"))
	index.Insert([]cacheindex.Chunk{chunk(1, 101), chunk(2, 21)}, wl("w2"))
	checkMatches(t, index.Select([]cacheindex.Chunk{chunk(1, 102), chunk(2, 22)}), map[string]uint64{
		"w1": 124, "w2": 124,
	})
}

// TestSelectZeroTokenChunksAndResultOwnership 验证零 token Chunk 参与结构
// 匹配但不产生正长度候选，且修改返回结果不影响后续查询。
func TestSelectZeroTokenChunksAndResultOwnership(t *testing.T) {
	index := cacheindex.New(10000)
	zero, a, b := chunk(1, 0), chunk(2, 100), chunk(3, 20)
	index.Insert([]cacheindex.Chunk{zero, a, b}, wl("w1"))
	checkMatches(t, index.Select([]cacheindex.Chunk{zero}), map[string]uint64{})
	checkMatches(t, index.Select([]cacheindex.Chunk{zero, a}), map[string]uint64{"w1": 100})

	first := index.Select([]cacheindex.Chunk{zero, a, b})
	first[0].Workload = wl("corrupted")
	first[0].Length = 1
	checkMatches(t, index.Select([]cacheindex.Chunk{zero, a, b}), map[string]uint64{"w1": 120})
}

// TestSelectDoesNotModifyQuery 验证 Select 只借用输入，不修改查询切片。
func TestSelectDoesNotModifyQuery(t *testing.T) {
	index := cacheindex.New(10000)
	key := []cacheindex.Chunk{chunk(1, 100), chunk(2, 20)}
	index.Insert(key, wl("w1"))
	query := []cacheindex.Chunk{key[0], key[1], chunk(3, 30)}
	want := append([]cacheindex.Chunk(nil), query...)
	index.Select(query)
	if !reflect.DeepEqual(query, want) {
		t.Fatalf("Select changed input: got %v, want %v", query, want)
	}
}

// TestSelectAgainstPrefixOracle 用随机插入的完整 key 独立计算每个 workload
// 的最长公共 Chunk 前缀作为基准，交叉验证 Select 结果；基准不依赖树的分裂方式。
func TestSelectAgainstPrefixOracle(t *testing.T) {
	index := cacheindex.New(10000)
	rng := rand.New(rand.NewSource(7))
	workloads := []cacheindex.Workload{wl("w1"), wl("w2"), wl("w3")}
	keys := make(map[string][][]cacheindex.Chunk)
	var allKeys [][]cacheindex.Chunk
	for i := 0; i < 60; i++ {
		key := make([]cacheindex.Chunk, 2+rng.Intn(5))
		for j := range key {
			key[j] = chunk(uint64(1+rng.Intn(5)), uint64(1+rng.Intn(30)))
		}
		workload := workloads[rng.Intn(len(workloads))]
		index.Insert(key, workload)
		keys[workload.ID] = append(keys[workload.ID], key)
		allKeys = append(allKeys, key)
	}
	for i := 0; i < 200; i++ {
		var query []cacheindex.Chunk
		if i%3 == 0 {
			query = make([]cacheindex.Chunk, rng.Intn(8))
			for j := range query {
				query[j] = chunk(uint64(1+rng.Intn(5)), uint64(1+rng.Intn(30)))
			}
		} else {
			stored := allKeys[rng.Intn(len(allKeys))]
			query = append([]cacheindex.Chunk(nil), stored[:rng.Intn(len(stored)+1)]...)
			if i%2 == 0 {
				query = append(query, chunk(99, 17))
			}
		}
		want := make(map[string]uint64)
		for workloadID, histories := range keys {
			for _, history := range histories {
				var length uint64
				for j := 0; j < len(query) && j < len(history) && query[j].Hash == history[j].Hash; j++ {
					length += query[j].TokenCount
				}
				if length > want[workloadID] {
					want[workloadID] = length
				}
			}
		}
		checkMatches(t, index.Select(query), want)
	}
}

// TestInsertSplitsWithoutChangingOtherWorkloads 验证插入在压缩段中间分叉
// 引发分裂时，已有 workload 的驻留关系和匹配长度不受影响。
func TestInsertSplitsWithoutChangingOtherWorkloads(t *testing.T) {
	index := cacheindex.New(10000)
	a, b, c, x := chunk(1, 100), chunk(2, 20), chunk(3, 30), chunk(4, 40)
	index.Insert([]cacheindex.Chunk{a, b, c}, wl("w1"))
	index.Insert([]cacheindex.Chunk{a, b, x}, wl("w2"))
	checkMatches(t, index.Select([]cacheindex.Chunk{a, b, c}), map[string]uint64{"w1": 150, "w2": 120})
	checkMatches(t, index.Select([]cacheindex.Chunk{a, b, x}), map[string]uint64{"w2": 160, "w1": 120})
}

// 分裂已有分叉的节点时，原有后缀仍应可查询；多分叉的子节点也不能丢失。
func TestInsertSplitsBranchingNode(t *testing.T) {
	index := cacheindex.New(10000)
	a, b, c := chunk(1, 100), chunk(2, 20), chunk(3, 30)
	for i := uint64(10); i < 20; i++ {
		index.Insert([]cacheindex.Chunk{a, b, c, chunk(i, 1)}, wl("w1"))
	}
	index.Insert([]cacheindex.Chunk{a, b}, wl("w2"))

	for i := uint64(10); i < 20; i++ {
		checkMatches(t, index.Select([]cacheindex.Chunk{a, b, c, chunk(i, 1)}), map[string]uint64{
			"w1": 151, "w2": 120,
		})
	}
}

// 同一 workload 的追加、分叉和短前缀重插入不能丢失已有的长前缀。
func TestInsertSameWorkloadAppendBranchAndRepeat(t *testing.T) {
	index := cacheindex.New(10000)
	a, b, c, x, y := chunk(1, 100), chunk(2, 20), chunk(3, 30), chunk(4, 40), chunk(5, 50)
	ab := []cacheindex.Chunk{a, b}
	abc := []cacheindex.Chunk{a, b, c}
	abx := []cacheindex.Chunk{a, b, x}
	workload := wl("w1")

	index.Insert(ab, workload)
	index.Insert(abc, workload)
	index.Insert(abx, workload)
	index.Insert(ab, workload)

	checkMatches(t, index.Select(abc), map[string]uint64{"w1": 150})
	checkMatches(t, index.Select(abx), map[string]uint64{"w1": 160})
	checkMatches(t, index.Select([]cacheindex.Chunk{a, b, y}), map[string]uint64{"w1": 120})
}

// TestInsertCopiesItsInput 验证 Insert 取得输入的自有副本：调用方在 Insert
// 返回后复用或改写输入数组，不影响树内已登记的前缀。
func TestInsertCopiesItsInput(t *testing.T) {
	index := cacheindex.New(10000)
	a, b := chunk(1, 100), chunk(2, 20)
	key := make([]cacheindex.Chunk, 2, 4)
	copy(key, []cacheindex.Chunk{a, b})
	index.Insert(key, wl("w1"))
	key[0] = chunk(8, 100)
	key[1] = chunk(9, 20)
	key = append(key, chunk(10, 30))
	checkMatches(t, index.Select([]cacheindex.Chunk{a, b}), map[string]uint64{"w1": 120})
}

// TestInsertZeroTokenChunksStillPreservePath 验证零 token Chunk 仍参与
// 结构匹配和驻留关系：零 token 前缀不产生候选，但不阻断后续 Chunk 的命中。
func TestInsertZeroTokenChunksStillPreservePath(t *testing.T) {
	index := cacheindex.New(10000)
	zero, a := chunk(1, 0), chunk(2, 100)
	index.Insert([]cacheindex.Chunk{zero}, wl("w1"))
	index.Insert([]cacheindex.Chunk{zero, a}, wl("w1"))
	checkMatches(t, index.Select([]cacheindex.Chunk{zero, a}), map[string]uint64{"w1": 100})
	checkMatches(t, index.Select([]cacheindex.Chunk{chunk(9, 0), a}), map[string]uint64{})
}

// TestInsertWorkloadIdentityByID 验证 Workload 身份只按 ID 匹配：同 ID
// 不同 Group/Capacity 是同一 workload，只输出一次，随附属性随
// 最近一次登记刷新（Capacity 动态更新）；不同 ID 则是不同
// workload。
func TestInsertWorkloadIdentityByID(t *testing.T) {
	index := cacheindex.New(10000)
	key := []cacheindex.Chunk{chunk(1, 100)}
	index.Insert(key, cacheindex.Workload{ID: "a-1", Group: "a", Capacity: 100})
	index.Insert(key, cacheindex.Workload{ID: "a-1", Group: "b", Capacity: 200})
	index.Insert(key, wl("b-1"))

	got := index.Select(key)
	if len(got) != 2 {
		t.Fatalf("Select = %+v, want 2 workloads", got)
	}
	byID := make(map[string]cacheindex.Workload, len(got))
	for _, match := range got {
		byID[match.Workload.ID] = match.Workload
	}
	if a, ok := byID["a-1"]; !ok || a.Group != "b" || a.Capacity != 200 {
		t.Fatalf("workload a-1 = %+v, want latest Group \"b\" and Capacity 200", a)
	}
	if _, ok := byID["b-1"]; !ok {
		t.Fatalf("Select = %+v, want workload b-1", got)
	}
	if gotMatch := index.Select([]cacheindex.Chunk{chunk(9, 100)}); len(gotMatch) != 0 {
		t.Fatalf("Select = %+v, want no match", gotMatch)
	}
}

// 并发读写允许读到旧路径或较短匹配；写入结束后全部前缀必须可见。
// 与 go test -race 一起运行，用于检查 Select 与 Insert 的数据竞争。
func TestConcurrentSelectAndInsert(t *testing.T) {
	index := cacheindex.New(10000)
	workload := wl("w1")
	a := chunk(1, 100)
	const writers, readers, keysPerWriter = 4, 4, 80
	const totalKeys = writers * keysPerWriter
	start := make(chan struct{})
	var wg sync.WaitGroup

	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			<-start
			for i := 0; i < keysPerWriter; i++ {
				id := writer*keysPerWriter + i
				index.Insert([]cacheindex.Chunk{a, chunk(uint64(id+2), 20)}, workload)
			}
		}(writer)
	}
	for reader := 0; reader < readers; reader++ {
		wg.Add(1)
		go func(reader int) {
			defer wg.Done()
			<-start
			for i := 0; i < totalKeys*3; i++ {
				id := (reader + i) % totalKeys
				got := index.Select([]cacheindex.Chunk{a, chunk(uint64(id+2), 20)})
				if len(got) == 0 {
					continue
				}
				if len(got) != 1 || got[0].Workload != workload || (got[0].Length != 100 && got[0].Length != 120) {
					t.Errorf("concurrent Select returned invalid match: %+v", got)
					return
				}
			}
		}(reader)
	}
	close(start)
	wg.Wait()

	for id := 0; id < totalKeys; id++ {
		key := []cacheindex.Chunk{a, chunk(uint64(id+2), 20)}
		checkMatches(t, index.Select(key), map[string]uint64{"w1": 120})
	}
}
