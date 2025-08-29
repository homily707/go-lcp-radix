package lradix

import (
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// generateRandomKey 生成具有随机性但保持匹配性的 key
// 平均长度 4k，范围 1-16k
func generateRandomKey(base string, counter int) []byte {
	r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(counter)))

	// 生成随机长度，使用正态分布让平均值在 4k 附近
	// 最小值 1，最大值 16k (16384)
	length := 1 + int(r.NormFloat64()*2000+4000)
	if length < 1 {
		length = 1
	}
	if length > 16384 {
		length = 16384
	}

	// 生成基础 key
	baseKey := fmt.Sprintf("%s-%d", base, counter)

	// 如果基础 key 已经超过目标长度，截取
	if len(baseKey) >= length {
		return []byte(baseKey[:length])
	}

	// 否则用随机字符填充到目标长度
	result := make([]byte, length)
	copy(result, baseKey)

	// 填充剩余部分，使用可打印字符确保匹配性
	for i := len(baseKey); i < length; i++ {
		// 使用字母数字和常见符号，确保有匹配性
		charset := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_."
		result[i] = charset[r.Intn(len(charset))]
	}

	return result
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// generateMatchingKey 生成与给定 key 有匹配性的 key
func generateMatchingKey(baseKey []byte, variation int) []byte {
	r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(variation)))

	// 生成随机长度，使用正态分布让平均值在 4k 附近
	length := 1 + int(r.NormFloat64()*2000+4000)
	if length < 1 {
		length = 1
	}
	if length > 16384 {
		length = 16384
	}

	// 计算匹配长度 (40%-100% 的较短长度)
	matchLen := min(len(baseKey), length)
	if matchLen > 1 {
		matchLen = r.Intn(matchLen*3/5) + matchLen*2/5 // 40%-100% 的匹配
	} else if matchLen == 1 {
		// 如果基础 key 长度为 1，有 50% 概率匹配
		if r.Intn(2) == 0 {
			matchLen = 1
		} else {
			matchLen = 0
		}
	} else {
		matchLen = 0
	}

	result := make([]byte, length)

	// 复制匹配部分
	if matchLen > 0 {
		copy(result, baseKey[:matchLen])
	}

	// 填充剩余部分
	for i := matchLen; i < length; i++ {
		charset := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_."
		result[i] = charset[r.Intn(len(charset))]
	}

	return result
}

func TestConcurrentTreeBasicUsage(t *testing.T) {
	// 1. 测试树和节点创建
	tree := NewConcurrentTree[rune, int]()
	if tree.Root == nil {
		t.Error("Expected root node to be created")
	}

	tree.Insert([]rune("hello"), 1)      // 2. 测试基本插入功能
	tree.Insert([]rune("world"), 2)      // 3. 测试无冲突插入
	tree.Insert([]rune("helloworld"), 3) // 4. 测试前缀冲突插入（需要分裂节点）
	tree.Insert([]rune("hello"), 4)      // 5. 测试完全覆盖插入
	tree.Insert([]rune("he"), 5)         // 6. 测试部分覆盖插入（插入是已存在键的前缀）
	tree.Insert([]rune{}, 6)             // 7. 测试空字符串插入
	tree.Insert([]rune{'a'}, 7)          // 8. 测试单字符插入

	// fmt.Println(tree.String())
	testCases := []struct {
		input  string
		expect int
	}{
		{"hello", 4},       // 9. 测试完全匹配
		{"helloworld!", 3}, // 10. 测试前缀匹配
		{"helllllllll", 4}, // 11. 测试部分匹配
		{"你好", 0},          // 12. 测试无匹配
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]rune(tc.input))
		if result == nil && tc.expect != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expect)
		} else if result != nil && *result != tc.expect {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expect)
		}
	}
}

func TestConcurrentTreeRemoveNode(t *testing.T) {
	// 1. 测试树和节点创建
	tree := NewConcurrentTree[rune, int]()
	if tree.Root == nil {
		t.Error("Expected root node to be created")
	}

	// 2. 测试基本插入功能
	node1 := tree.Insert([]rune("hello"), 1)
	tree.Insert([]rune("world"), 2)
	tree.Insert([]rune("helloworld"), 3)
	tree.Insert([]rune("help"), 4)
	helper := tree.Insert([]rune("helper"), 5)
	woooo := tree.Insert([]rune("wooo"), 6)

	// fmt.Println(tree.String())
	// 测试删除叶节点
	tree.RemoveNode(helper)
	_, result, _ := tree.LongestCommonPrefixMatch([]rune("helper"))
	if result == nil || *result != 4 {
		t.Errorf("LCP(helper) = %v, expected 4", *result)
	}

	// 测试删除共享节点
	tree.RemoveNode(node1)
	tree.RemoveNode(woooo)
	testCases := []struct {
		input  string
		expect int
	}{
		{"hello", 3},       // 测试删除节点匹配
		{"world", 2},       // 测试完全匹配
		{"helloworld!", 3}, // 测试前缀匹配
		{"help", 4},        // 测试完全匹配
		{"helper!", 4},     // 测试前缀匹配
		{"worl", 2},
		{"wooo", 2},
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]rune(tc.input))
		if result == nil && tc.expect != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expect)
		} else if result != nil && *result != tc.expect {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expect)
		}
	}
}

// 并发插入测试
func TestConcurrentInsert(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()
	var wg sync.WaitGroup

	// 准备不同类型的键
	keys := []struct {
		key   string
		value int
	}{
		{"apple", 1},
		{"banana", 2},
		{"orange", 3},
		{"hello", 4},
		{"help", 5},
		{"helloworld", 6},
		{"hello", 7},
		{"hel", 8},
	}

	for _, key := range keys {
		wg.Add(1)
		go func(k string, v int) {
			defer wg.Done()
			tree.Insert([]rune(k), v)
		}(key.key, key.value)
	}
	wg.Wait()
	nodeNums := strings.Count(tree.String(), "└──")
	if nodeNums != 8 {
		t.Errorf("Expected %d nodes, got %d", 8, nodeNums)
	}
}

func TestConcurrentWriteRead(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()
	tree.Insert([]rune("hello"), 1)
	keys := []struct {
		key    string
		action string
		value  int
	}{
		// 基础前缀测试
		{"help", "insert", 2},
		{"helloworld", "insert", 3},
		{"hello", "lcp", 1},
		{"hel", "lcp", 1},

		// 更复杂的 keys 测试
		{"helloworld123", "insert", 4}, // 扩展已有路径
		{"helpdesk", "insert", 5},      // 分支扩展
		{"helloworld123456", "lcp", 0}, // 长路径匹配
		{"helpdeskmanager", "lcp", 0},  // 分支匹配
		{"hexagon", "insert", 6},       // 共享前缀但不同路径
		{"healing", "insert", 7},       // 另一个共享前缀
		{"hex", "lcp", 0},              // 部分匹配
	}

	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func(k string, action string, v int) {
			defer wg.Done()
			switch action {
			case "insert":
				tree.Insert([]rune(k), v)
			case "lcp":
				tree.LongestCommonPrefixMatch([]rune(k))
			}
		}(key.key, key.action, key.value)
	}
	wg.Wait()
	//fmt.Println(tree.String())

	// 验证最终树结构的一致性
	testCases := []struct {
		input    string
		expected int
	}{
		{"help", 2},
		{"helloworld", 3},
		{"helloworld123", 4},
		{"helpdesk", 5},
		{"hexagon", 6},
		{"healing", 7},

		// 前缀匹配测试
		{"helloworld123456", 4},
		{"helpdeskmanager", 5},
		{"hex", 6},
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]rune(tc.input))
		if result == nil {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

func TestConcurrentWriteReadRemove(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()
	tree.Insert([]rune("hello"), 1)
	node2 := tree.Insert([]rune("help"), 2)
	node3 := tree.Insert([]rune("helper"), 3)

	keys := []struct {
		node   *ConcurrentNode[rune, int]
		key    string
		action string
		value  int
	}{
		{nil, "help!help!", "insert", 2},
		{nil, "helloworld", "insert", 3},
		{nil, "hi", "insert", 4},
		{node2, "", "remove", 0},
		{nil, "hello", "lcp", 0},
		{nil, "hey", "lcp", 0},
		{node3, "", "remove", 0},
	}

	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func(k string, action string, v int) {
			defer wg.Done()
			switch action {
			case "insert":
				tree.Insert([]rune(k), v)
			case "lcp":
				tree.LongestCommonPrefixMatch([]rune(k))
			case "remove":
				tree.RemoveNode(key.node)
			}
		}(key.key, key.action, key.value)
	}
	wg.Wait()
	// Test LCP matches
	testCases := []struct {
		input    string
		expected int
	}{
		{"hello", 1},
		{"helloworkd", 3},
		{"hi", 4},
		{"help", 2},
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]rune(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

// 基准测试：并发插入性能
func BenchmarkConcurrentInsertParallel(b *testing.B) {
	tree := NewConcurrentTree[byte, int]()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := generateRandomKey("parallel-key", i)
			tree.Insert(key, i)
			i++
		}
	})
}

// 基准测试：并发读取性能
func BenchmarkConcurrentReadParallel(b *testing.B) {
	tree := NewConcurrentTree[byte, int]()

	// 预插入数据
	var keys [][]byte
	for i := 0; i < 10000; i++ {
		key := generateRandomKey("parallel-read-key", i)
		keys = append(keys, key)
		tree.Insert(key, i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			tree.LongestCommonPrefixMatch(keys[i%10000])
			i++
		}
	})
}

// 基准测试：混合读写性能
func BenchmarkConcurrentMixed(b *testing.B) {
	tree := NewConcurrentTree[byte, int]()

	// 预插入一些基础数据
	var baseKeys [][]byte
	for i := 0; i < 1000; i++ {
		key := generateRandomKey("mixed-base-key", i)
		baseKeys = append(baseKeys, key)
		tree.Insert(key, i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%4 == 0 {
				// 插入操作
				key := generateRandomKey("mixed-insert-key", i)
				tree.Insert(key, i)
			} else {
				// 读取操作
				tree.LongestCommonPrefixMatch(baseKeys[i%1000])
			}
			i++
		}
	})
}

// 基准测试：大数据量下的性能
func BenchmarkConcurrentLargeDataset(b *testing.B) {
	tree := NewConcurrentTree[byte, int]()

	// 预插入大量数据
	baseSize := 5000
	var baseKeys [][]byte
	for i := 0; i < baseSize; i++ {
		key := generateRandomKey("large-key", i)
		baseKeys = append(baseKeys, key)
		tree.Insert(key, i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// 交替进行读写操作
			if i%2 == 0 {
				// 读取操作
				tree.LongestCommonPrefixMatch(baseKeys[rand.Intn(baseSize)])
			} else {
				// 插入操作
				key := generateRandomKey("large-new-key", i)
				tree.Insert(key, i+baseSize)
			}
			i++
		}
	})
}

// 基准测试：并发删除操作性能
func BenchmarkConcurrentRemove(b *testing.B) {
	// 预插入数据
	tree := NewConcurrentTree[byte, int]()
	var nodes []*ConcurrentNode[byte, int]
	for i := 0; i < 10000; i++ {
		key := generateRandomKey("remove-benchmark", i)
		node := tree.Insert(key, i)
		nodes = append(nodes, node)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i < len(nodes) {
				tree.RemoveNode(nodes[i])
			}
			i++
		}
	})

	b.StopTimer()

	// 内存使用量统计
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	b.ReportMetric(float64(m.Alloc)/1024, "alloc-kb")
	b.ReportMetric(float64(m.TotalAlloc)/1024, "total-alloc-kb")
	b.ReportMetric(float64(m.Sys)/1024, "sys-kb")
	b.ReportMetric(float64(m.NumGC), "gc-cycles")
}
