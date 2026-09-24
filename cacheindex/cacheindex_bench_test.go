package cacheindex_test

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/homily707/go-lcp-radix/cacheindex"
)

// benchSink 供串行基准防止编译器把纯读操作优化掉。并行基准不得写它：
// 多 goroutine 共写既是数据竞争，也会引入与树无关的共享缓存行争用，
// 干扰并发性能测量；它们改用每 goroutine 的局部 sink。
var benchSink []cacheindex.Match

// benchRand 是固定种子的 splitmix64，负载可复现，逐次输出的 hash 互不相同，
// 不会在基准里引入意外碰撞。
type benchRand uint64

func (r *benchRand) next() uint64 {
	*r += 0x9e3779b97f4a7c15
	z := uint64(*r)
	z ^= z >> 30
	z *= 0xbf58476d1ce4e5b9
	z ^= z >> 27
	z *= 0x94d049bb133111eb
	z ^= z >> 31
	return z
}

// chunks 生成 n 个 hash 互不相同的 Chunk，TokenCount 统一为 tokens。
func (r *benchRand) chunks(n int, tokens uint64) []cacheindex.Chunk {
	key := make([]cacheindex.Chunk, n)
	for i := range key {
		key[i] = cacheindex.Chunk{Hash: r.next(), TokenCount: tokens}
	}
	return key
}

// buildSessions 构造"公共 prompt + 每会话独立后缀"的负载：sessions 个会话共享
// 前 promptLen 个 Chunk，之后各有 tailLen 个自己的 Chunk。每个会话登记给独立
// workload，使公共前缀节点聚集全部 values，后缀分叉集中在 prompt 末端节点上。
func buildSessions(sessions, promptLen, tailLen int) (*cacheindex.Tree, [][]cacheindex.Chunk, []cacheindex.Workload) {
	r := benchRand(0x1234)
	index := cacheindex.New(10)
	prompt := r.chunks(promptLen, 100)
	keys := make([][]cacheindex.Chunk, sessions)
	workloads := make([]cacheindex.Workload, sessions)
	for i := range keys {
		key := make([]cacheindex.Chunk, 0, promptLen+tailLen)
		key = append(key, prompt...)
		key = append(key, r.chunks(tailLen, 100)...)
		keys[i] = key
		workloads[i] = wl(fmt.Sprintf("w%d", i))
		index.Insert(key, workloads[i])
	}
	return index, keys, workloads
}

// BenchmarkSelect 在共享 prompt 负载下测量几种查询形态：完整命中（最深路径、
// 最多返回 10 个 workload 候选）、段内截断、末 Chunk 失配（走完路径但少一层
// 候选）以及首 Chunk 失配（根即停、零候选）。
func BenchmarkSelect(b *testing.B) {
	for _, tc := range []struct {
		name                         string
		sessions, promptLen, tailLen int
	}{
		{"sessions=100/prompt=8/tail=32", 100, 8, 32},
		{"sessions=1000/prompt=8/tail=32", 1000, 8, 32},
		{"sessions=4000/prompt=16/tail=64", 4000, 16, 64},
	} {
		index, keys, _ := buildSessions(tc.sessions, tc.promptLen, tc.tailLen)
		r := benchRand(0xbeef)
		partial := make([][]cacheindex.Chunk, len(keys))
		deepMiss := make([][]cacheindex.Chunk, len(keys))
		miss := make([][]cacheindex.Chunk, len(keys))
		for i, key := range keys {
			partial[i] = key[:tc.promptLen+tc.tailLen/2]
			deepMiss[i] = append(append([]cacheindex.Chunk(nil), key[:len(key)-1]...), r.chunks(1, 100)...)
			miss[i] = append(r.chunks(1, 100), key[1:]...)
		}
		for _, shape := range []struct {
			name    string
			queries [][]cacheindex.Chunk
		}{
			{"full", keys},
			{"partial", partial},
			{"deep-miss", deepMiss},
			{"miss", miss},
		} {
			b.Run(tc.name+"/"+shape.name, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchSink = index.Select(shape.queries[i%len(shape.queries)])
				}
			})
		}
	}
}

// BenchmarkSelectParallel 测量共享 prompt 负载下并发读的伸缩性；读锁只递增
// 计数器，结果主要反映缓存局部性而非锁争用。
func BenchmarkSelectParallel(b *testing.B) {
	index, keys, _ := buildSessions(1000, 8, 32)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		var sink []cacheindex.Match
		for pb.Next() {
			sink = index.Select(keys[i%len(keys)])
			i++
		}
		runtime.KeepAlive(sink)
	})
}

// BenchmarkInsertNewSessionsFromScratch 每轮迭代从空树重建并登记全部会话；
// ns/op 是整棵树的构建成本，覆盖新叶子创建、公共 prompt 上的分叉以及
// children 从 slice 到 map 的扇出升级。
func BenchmarkInsertNewSessionsFromScratch(b *testing.B) {
	for _, tc := range []struct {
		name                         string
		sessions, promptLen, tailLen int
	}{
		{"sessions=100/prompt=8/tail=32", 100, 8, 32},
		{"sessions=1000/prompt=8/tail=32", 1000, 8, 32},
		{"sessions=4000/prompt=16/tail=64", 4000, 16, 64},
	} {
		r := benchRand(0x5678)
		prompt := r.chunks(tc.promptLen, 100)
		keys := make([][]cacheindex.Chunk, tc.sessions)
		workloads := make([]cacheindex.Workload, tc.sessions)
		for i := range keys {
			key := make([]cacheindex.Chunk, 0, tc.promptLen+tc.tailLen)
			key = append(key, prompt...)
			key = append(key, r.chunks(tc.tailLen, 100)...)
			keys[i] = key
			workloads[i] = wl(fmt.Sprintf("w%d", i))
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				index := cacheindex.New(10)
				b.StartTimer()
				for j := range keys {
					index.Insert(keys[j], workloads[j])
				}
			}
		})
	}
}

// BenchmarkInsertReRegister 测量稳态重复登记：key 已完整驻留于同一 workload，
// Insert 走完整定位与 values 去重后直接返回，不修改结构。
func BenchmarkInsertReRegister(b *testing.B) {
	index, keys, workloads := buildSessions(1000, 8, 32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		index.Insert(keys[i%len(keys)], workloads[i%len(keys)])
	}
}

// BenchmarkInsertAppend 模拟多轮对话追加：同一 workload 独占的叶子逐轮延长，
// 合并追加省去逐轮新节点，但每次 Insert 仍需定位并比较整个已积累的 key
// （O(M)），单次成本随会话变长而增长。会话达到 maxRounds 轮后换新会话，
// 控制单叶规模和树的总大小。
func BenchmarkInsertAppend(b *testing.B) {
	const (
		initialChunks = 8
		maxRounds     = 200
	)
	for _, roundLen := range []int{1, 4} {
		b.Run(fmt.Sprintf("roundChunks=%d", roundLen), func(b *testing.B) {
			r := benchRand(0x9abc)
			index := cacheindex.New(10)
			workload := wl("chat")
			key := r.chunks(initialChunks, 100)
			index.Insert(key, workload)
			rounds := 0
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key = append(key, r.chunks(roundLen, 100)...)
				index.Insert(key, workload)
				if rounds++; rounds == maxRounds {
					key = r.chunks(initialChunks, 100)
					index.Insert(key, workload)
					rounds = 0
				}
			}
		})
	}
}

// BenchmarkInsertSplit 每轮迭代先登记一条独占长叶，再按从小到大的分叉点成批
// 插入不同 workload 的 key，每次插入都在段内触发一次分裂；ns/op 是整批
// 分裂插入的成本。
func BenchmarkInsertSplit(b *testing.B) {
	const (
		baseChunks = 256
		splits     = 64
	)
	r := benchRand(0xdef0)
	base := r.chunks(baseChunks, 100)
	splitKeys := make([][]cacheindex.Chunk, splits)
	workloads := make([]cacheindex.Workload, splits)
	for i := range splitKeys {
		k := (i + 1) * baseChunks / (splits + 1)
		key := append([]cacheindex.Chunk(nil), base[:k]...)
		splitKeys[i] = append(key, r.chunks(4, 100)...)
		workloads[i] = wl(fmt.Sprintf("split%d", i))
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		index := cacheindex.New(10)
		index.Insert(base, wl("base"))
		b.StartTimer()
		for j := range splitKeys {
			index.Insert(splitKeys[j], workloads[j])
		}
	}
}

// BenchmarkMixedSelectInsert 混合读写：并行 goroutine 以 1:7 的比例穿插重复
// 登记写与查询，测量整树 RWMutex 下写锁对并发读的实际阻塞。写为重复登记，
// 不改变树的大小。
func BenchmarkMixedSelectInsert(b *testing.B) {
	index, keys, workloads := buildSessions(1000, 8, 32)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		var sink []cacheindex.Match
		for pb.Next() {
			if i%8 == 0 {
				index.Insert(keys[i%len(keys)], workloads[i%len(keys)])
			} else {
				sink = index.Select(keys[i%len(keys)])
			}
			i++
		}
		runtime.KeepAlive(sink)
	})
}
