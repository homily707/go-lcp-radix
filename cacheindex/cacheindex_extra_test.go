package cacheindex_test

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/homily707/go-lcp-radix/cacheindex"
)

type registration struct {
	key      []cacheindex.Chunk
	workload cacheindex.Workload
}

// workloadExpectation 是 oracle 对一个 workload 的期望：最佳匹配长度
// 与最近一次登记的 Workload 属性。
type workloadExpectation struct {
	workload cacheindex.Workload
	length   uint64
}

// 直接对完整历史求 LCP，不依赖树结构；身份按 ID 比较，每个 ID 取
// 最大匹配长度，Workload 属性取最近一次登记：属性刷新不依赖本次
// 登记是否在该查询上产生候选。
func prefixMatches(history []registration, query []cacheindex.Chunk) map[string]workloadExpectation {
	want := make(map[string]workloadExpectation)
	for _, entry := range history {
		var length uint64
		for i := 0; i < len(query) && i < len(entry.key) && query[i].Hash == entry.key[i].Hash; i++ {
			length += query[i].TokenCount
		}
		if e, ok := want[entry.workload.ID]; ok {
			e.workload = entry.workload
			e.length = max(e.length, length)
			want[entry.workload.ID] = e
		} else if length > 0 {
			want[entry.workload.ID] = workloadExpectation{entry.workload, length}
		}
	}
	return want
}

func checkWorkloadMatches(t *testing.T, got []cacheindex.Match, want map[string]workloadExpectation) {
	t.Helper()
	seen := make(map[string]workloadExpectation, len(got))
	for i, match := range got {
		if match.Length == 0 || (i > 0 && got[i-1].Length < match.Length) {
			t.Errorf("invalid match lengths: %+v", got)
		}
		if _, exists := seen[match.Workload.ID]; exists {
			t.Errorf("duplicate workload: %+v", match.Workload)
		}
		seen[match.Workload.ID] = workloadExpectation{match.Workload, match.Length}
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("Select = %v, want %v", seen, want)
	}
}

// TestInsertSameIDMergesAcrossAttributeChanges 验证同 ID 不同
// Group/Capacity 的登记合并为同一 workload：取最佳匹配长度，
// 属性随最近一次登记刷新；不同字段值不再派生新身份。
func TestInsertSameIDMergesAcrossAttributeChanges(t *testing.T) {
	index := cacheindex.New(10000)
	key := []cacheindex.Chunk{chunk(0, 10), chunk(1, 20), chunk(2, 30)}
	history := []registration{
		{key, cacheindex.Workload{ID: "same", Group: "a", Capacity: 100}},
		{key[:2], cacheindex.Workload{ID: "same", Group: "b", Capacity: 100}},
		{key[:1], cacheindex.Workload{ID: "same", Group: "a", Capacity: 200}},
		{key, cacheindex.Workload{}},
	}
	for _, entry := range history {
		index.Insert(entry.key, entry.workload)
		index.Insert(entry.key, entry.workload)
	}
	index.Insert(nil, wl("empty"))
	index.Insert([]cacheindex.Chunk{}, wl("empty"))
	checkWorkloadMatches(t, index.Select(key), prefixMatches(history, key))
}

// 在不同扇出下替换已有边，检查分裂后所有兄弟分支仍可查询。
func TestInsertSplitsAcrossFanoutBoundary(t *testing.T) {
	for _, fanout := range []int{7, 8, 9, 16} {
		for _, sharedPrefix := range []bool{false, true} {
			t.Run(fmt.Sprintf("fanout=%d/shared=%t", fanout, sharedPrefix), func(t *testing.T) {
				index := cacheindex.New(10000)
				var history []registration
				for i := 0; i < fanout; i++ {
					key := []cacheindex.Chunk{chunk(uint64(i), 10), chunk(100, 20), chunk(101, 30)}
					if sharedPrefix {
						key = append([]cacheindex.Chunk{chunk(200, 5)}, key...)
					}
					history = append(history, registration{key, wl(fmt.Sprint(i))})
					index.Insert(key, history[i].workload)
				}
				for i := 0; i < fanout; i++ {
					key := history[i].key
					short := registration{key[:len(key)-1], wl("short")}
					branchKey := append([]cacheindex.Chunk(nil), short.key...)
					branch := registration{append(branchKey, chunk(102, 40)), wl("branch")}
					for _, entry := range []registration{short, branch} {
						index.Insert(entry.key, entry.workload)
						history = append(history, entry)
						for _, query := range history {
							checkWorkloadMatches(t, index.Select(query.key), prefixMatches(history, query.key))
						}
					}
				}
			})
		}
	}
}

// 零 token 层不能改变候选去重或同分行为，查询计数独立于登记计数。
func TestSelectZeroTokenLevelsAndQueryCounts(t *testing.T) {
	index := cacheindex.New(10000)
	key := []cacheindex.Chunk{chunk(0, 10), chunk(1, 0), chunk(2, 0), chunk(3, 20)}
	history := []registration{
		{key, wl("deep")},
		{key[:3], wl("middle")},
		{key[:2], wl("shallow")},
		{key[:1], wl("deep")},
	}
	for _, entry := range history {
		index.Insert(entry.key, entry.workload)
	}
	for _, query := range [][]cacheindex.Chunk{
		key, key[:3], key[:2],
		{chunk(0, 0), chunk(1, 0), chunk(2, 0), chunk(3, 0)},
		{chunk(0, 0), chunk(1, 5), chunk(2, 0), chunk(3, 7)},
		{chunk(0, 10), chunk(1, 0), chunk(99, 100)},
	} {
		checkWorkloadMatches(t, index.Select(query), prefixMatches(history, query))
	}
}

func TestInsertCopiesInputDuringSplitAndAppend(t *testing.T) {
	base := []cacheindex.Chunk{chunk(1, 10), chunk(2, 20), chunk(3, 30)}
	for _, tail := range []uint64{2, 4} {
		t.Run(fmt.Sprint(tail), func(t *testing.T) {
			index := cacheindex.New(10000)
			index.Insert(base, wl("original"))
			input := []cacheindex.Chunk{chunk(1, 11), chunk(tail, 21)}
			saved := append([]cacheindex.Chunk(nil), input...)
			index.Insert(input, wl("new"))
			for i := range input {
				input[i] = chunk(99, 99)
			}
			extended := append(saved, chunk(5, 40))
			index.Insert(extended, wl("new"))
			history := []registration{{base, wl("original")}, {append([]cacheindex.Chunk(nil), extended...), wl("new")}}
			for i := range extended {
				extended[i] = chunk(98, 98)
			}
			for _, entry := range history {
				checkWorkloadMatches(t, index.Select(entry.key), prefixMatches(history, entry.key))
			}
		})
	}
}

// 每条长路径从末端向根反复分裂，多个 workload 同时登记不同深度的分支。
// 并发期间只检查合法候选、完整 Chunk 计长和不高估；静止后与完整历史核对。
func TestConcurrentSelectAndRepeatedDeepSplits(t *testing.T) {
	const writers, depth = 4, 24
	index := cacheindex.New(10000)
	seed := wl("seed")
	var history []registration
	var writes [writers][]registration
	for writer := 0; writer < writers; writer++ {
		key := []cacheindex.Chunk{chunk(0, 3), chunk(uint64(writer+1), 5)}
		for i := 0; i < depth; i++ {
			key = append(key, chunk(uint64(i+10), uint64(i%4)))
		}
		index.Insert(key, seed)
		history = append(history, registration{key, seed})
		for n := depth; n > 0; n-- {
			branch := append([]cacheindex.Chunk(nil), key[:n+1]...)
			branch = append(branch, chunk(uint64(100+writer), 7))
			entry := registration{branch, wl(fmt.Sprint(writer))}
			writes[writer] = append(writes[writer], entry)
		}
	}
	baseline := append([]registration(nil), history...)
	for _, batch := range writes {
		history = append(history, batch...)
	}
	upper := make([]map[string]workloadExpectation, len(history))
	seedLengths := make([]uint64, len(history))
	validLengths := make([]map[uint64]bool, len(history))
	for i, entry := range history {
		upper[i] = prefixMatches(history, entry.key)
		seedLengths[i] = prefixMatches(baseline, entry.key)[seed.ID].length
		validLengths[i] = make(map[uint64]bool)
		var length uint64
		for _, c := range entry.key {
			length += c.TokenCount
			validLengths[i][length] = true
		}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for _, entry := range writes[writer] {
				index.Insert(entry.key, entry.workload)
			}
		}()
	}
	for reader := 0; reader < writers; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for round := 0; round < 10; round++ {
				for i, entry := range history {
					got := index.Select(entry.key)
					seen := make(map[string]uint64)
					for j, match := range got {
						if match.Length == 0 || !validLengths[i][match.Length] || match.Length > upper[i][match.Workload.ID].length ||
							seen[match.Workload.ID] != 0 || (j > 0 && got[j-1].Length < match.Length) {
							t.Errorf("invalid concurrent match for %v: %+v", entry.key, got)
							return
						}
						seen[match.Workload.ID] = match.Length
					}
					if seen[seed.ID] != seedLengths[i] {
						t.Errorf("seed match = %d, want %d", seen[seed.ID], seedLengths[i])
						return
					}
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	for i, entry := range history {
		checkWorkloadMatches(t, index.Select(entry.key), upper[i])
	}
}

// 用完整历史的 LCP 排名核对 TopK；同分不固定身份，但必须返回正确长度序列。
func TestSelectTopK(t *testing.T) {
	key := make([]cacheindex.Chunk, 120)
	for i := range key {
		key[i] = chunk(uint64(i), uint64(i%4))
	}
	var history []registration
	for i := 1; i <= 40; i++ {
		workload := wl(fmt.Sprintf("w%d", i))
		history = append(history, registration{key[:i*3], workload})
		// 同一 workload 在另一分支也驻留，不应影响最佳匹配与去重。
		branch := append([]cacheindex.Chunk(nil), key[:3]...)
		branch = append(branch, chunk(999, 7))
		history = append(history, registration{branch, workload})
	}
	// 最深层含多个同分候选，覆盖截断发生在同分集合内部的情况。
	history = append(history, registration{key, wl("tied")})
	zero := append([]cacheindex.Chunk(nil), key...)
	for i := range zero {
		zero[i].TokenCount = 0
	}
	queries := [][]cacheindex.Chunk{
		key, key[:5], key[:60], zero, nil,
		{chunk(9999, 10)},
		{key[0], key[1], chunk(9999, 10)},
		{key[0], key[1], key[2], chunk(999, 7)},
	}
	for _, k := range []int{-1, 0, 1, 2, 3, 10, 40, 41, 100, int(^uint(0) >> 1)} {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			index := cacheindex.New(k)
			for _, entry := range history {
				index.Insert(entry.key, entry.workload)
			}
			for _, query := range queries {
				want := prefixMatches(history, query)
				lengths := make([]uint64, 0, len(want))
				for _, e := range want {
					lengths = append(lengths, e.length)
				}
				slices.Sort(lengths)
				slices.Reverse(lengths)
				lengths = lengths[:min(max(k, 0), len(lengths))]
				got := index.Select(query)
				if len(got) != len(lengths) {
					t.Fatalf("query length %d: got %d matches, want %d", len(query), len(got), len(lengths))
				}
				seen := make(map[string]bool)
				for i, match := range got {
					if seen[match.Workload.ID] || match.Length != lengths[i] || match.Length != want[match.Workload.ID].length {
						t.Fatalf("query length %d: invalid TopK match %+v, want lengths %v", len(query), got, lengths)
					}
					seen[match.Workload.ID] = true
				}
			}
		})
	}
}
