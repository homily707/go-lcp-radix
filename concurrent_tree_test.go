package lradix

import (
	"fmt"
	"testing"
)

func Test_BasicUsage(t *testing.T) {
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

	fmt.Println(tree.String())
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

func Test_RemoveNode(t *testing.T) {
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

// TODO:
// 1. 并发插入测试

//   - 无冲突并发插入: 多个goroutine同时插入不共享前缀的键
//   - 前缀冲突并发插入: 多个goroutine同时插入共享前缀的键
//   - 相同键并发插入: 多个goroutine同时插入相同的键
//   - 混合并发插入: 不同类型的插入操作混合执行

//   2. 并发读写测试

//   - 并发插入和查询: 插入操作同时进行查询操作
//   - 并发删除和查询: 删除操作同时进行查询操作
//   - 并发全量操作: 插入、删除、查询同时进行

//   3. 锁竞争测试

//   - 高竞争插入: 大量goroutine竞争插入相同前缀的键
//   - 读写锁竞争: 同时进行大量的读和写操作
//   - 死锁预防测试: 验证复杂的锁获取顺序不会导致死锁

//   4. 数据一致性测试

//   - 最终一致性: 并发操作后树的状态应该是正确的
//   - 原子性验证: 单个操作要么完全成功要么完全失败
//   - 隔离性验证: 并发操作不应该相互影响

//   5. 性能和压力测试

//   - 基准测试: 测试单线程和多线程下的性能
//   - 压力测试: 大量并发操作下的稳定性
//   - 内存泄漏测试: 长时间运行下的内存使用情况

//   6. 边界条件测试

//   - 空树并发操作: 在空树上进行并发操作
//   - 大量重复键: 插入大量相同或相似的键
//   - 极长键: 插入很长的键测试锁的粒度
//   - 混合类型: 使用不同的K和T类型进行测试

//   7. 特定场景测试

//   - 节点分裂并发: 测试节点分裂过程中的并发安全性
//   - 节点删除并发: 测试节点删除过程中的并发安全性
//   - 树重构并发: 测试树结构变化过程中的并发安全性
