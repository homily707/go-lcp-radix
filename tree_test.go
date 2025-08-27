package lradix

import (
	"strings"
	"testing"
)

func TestInsert(t *testing.T) {
	tree := NewTree[byte, int]()

	// Test 1: Insert first element
	// Insert key-value pairs
	tree.Insert([]byte("hello"), 1)
	if tree.Root == nil {
		t.Fatal("Root should not be nil")
	}

	// Test 2: Insert multiple elements
	tree.Insert([]byte("hey"), 2)
	tree.Insert([]byte("hi"), 3)
	s := tree.String()
	// fmt.Println(s)
	num := strings.Count(s, "└──")
	if num != 6 {
		t.Errorf("Expected 6 node, got %d", num-1)
	}

	// Check that children exist
	if len(tree.Root.Children) != 1 {
		t.Errorf("Expected 1 child, got %d", len(tree.Root.Children))
	}

	// Test 3: Insert common prefix strings
	tree.Insert([]byte("help"), 4)
	tree.Insert([]byte("helper"), 5)

	// Should have more children now due to common prefix splitting
	if len(tree.Root.Children) == 0 {
		t.Error("Root should have children after inserting multiple strings")
	}
}

func TestLongestCommonPrefixMatchEmptyTree(t *testing.T) {
	tree := NewTree[byte, int]()
	_, result, isEnd := tree.LongestCommonPrefixMatch([]byte("test"))
	if result != nil {
		t.Errorf("Expected nil from empty tree, got %v", *result)
	}
	if isEnd {
		t.Errorf("Expected false for isEnd from empty tree, got %v", isEnd)
	}
	// Insert empty string should not affect the result
	tree.Insert([]byte(""), 42)
	_, result, isEnd = tree.LongestCommonPrefixMatch([]byte("whatever"))
	if result != nil {
		t.Errorf("Expected nil from empty tree, got %v", *result)
	}
	if isEnd {
		t.Errorf("Expected false for isEnd from empty tree, got %v", isEnd)
	}
}

func TestLongestCommonPrefixExactMatch(t *testing.T) {
	tree := NewTree[byte, int]()

	// Insert some test data
	tree.Insert([]byte("hello"), 1)
	tree.Insert([]byte("help"), 2)
	tree.Insert([]byte("world"), 3)

	// fmt.Println(tree.String())
	// Test cases for the third return value (isEnd)
	testCases := []struct {
		input    string
		expected bool
	}{
		{"hello", true},   // Exact match should return true
		{"help", true},    // Exact match should return true
		{"world", true},   // Exact match should return true
		{"hel", false},    // Partial match should return false
		{"he", false},     // Partial match should return false
		{"hell", false},   // Partial match should return false
		{"hellox", false}, // Full prefix match should return true for the matched node
		{"hel", false},    // Full prefix match should return true for the matched node
		{"worldx", false}, // Full prefix match should return true for the matched node
		{"test", false},   // No match should return false
		{"h", false},      // Partial match should return false
		{"w", false},      // Partial match should return false
	}

	for _, tc := range testCases {
		_, _, isEnd := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if isEnd != tc.expected {
			t.Errorf("LCP(%q) isEnd = %v, expected %v", tc.input, isEnd, tc.expected)
		}
	}
}

func TestComplexPrefixSplitting(t *testing.T) {
	tree := NewTree[byte, int]()

	// Insert strings that will create complex prefix splitting scenarios
	tree.Insert([]byte("romane"), 1)
	tree.Insert([]byte("romanus"), 2)
	tree.Insert([]byte("romulus"), 3)
	tree.Insert([]byte("rubens"), 4)
	tree.Insert([]byte("ruber"), 5)
	tree.Insert([]byte("rubicon"), 6)
	tree.Insert([]byte("rubicundus"), 7)

	// Verify the tree structure
	// fmt.Println("Complex prefix splitting tree:")
	// fmt.Println(tree.String())

	// Test LCP matches
	testCases := []struct {
		input    string
		expected int
	}{
		{"romane", 1},
		{"romanus", 2},
		{"romulus", 3},
		{"rubens", 4},
		{"ruber", 5},
		{"rubicon", 6},
		{"rubicundus", 7},
		// least recent prefix
		{"rom", 3},  // Should match romulus branch
		{"rub", 6},  // Should match rubicon branch
		{"rubi", 7}, // Should match rubicon branch
		{"roma", 2}, // Should match romanus branch
		{"rube", 5}, // Should match ruber branch
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

func TestNestedPrefixSplitting(t *testing.T) {
	tree := NewTree[byte, int]()

	// Create deeply nested prefix scenarios
	tree.Insert([]byte("a"), 1)
	tree.Insert([]byte("abc"), 3)
	tree.Insert([]byte("ab"), 2)
	tree.Insert([]byte("abcd"), 4)
	tree.Insert([]byte("abcde"), 5)
	tree.Insert([]byte("abcdef"), 6)

	// fmt.Println("Nested prefix tree:")
	// fmt.Println(tree.String())

	// Test that each prefix correctly resolves to the deepest match
	testCases := []struct {
		input    string
		expected int
	}{
		{"a", 1},
		{"ab", 2},
		{"abc", 3},
		{"abcd", 4},
		{"abcde", 5},
		{"abcdef", 6},
		{"abcdefg", 6}, // Should match the longest prefix
		{"abcx", 3},    // Should match abc
		{"abx", 2},     // Should match ab
		{"ax", 1},      // Should match a
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

func TestMultipleBranchesAtSameLevel(t *testing.T) {
	tree := NewTree[byte, int]()

	// Insert strings that create multiple branches from the same parent
	tree.Insert([]byte("test"), 1)
	tree.Insert([]byte("team"), 2)
	tree.Insert([]byte("toast"), 3)
	tree.Insert([]byte("taco"), 4)
	tree.Insert([]byte("tackle"), 5)

	// fmt.Println("Multiple branches tree:")
	// fmt.Println(tree.String())

	// Test LCP matches
	testCases := []struct {
		input    string
		expected int
	}{
		{"test", 1},
		{"team", 2},
		{"toast", 3},
		{"taco", 4},
		{"tackle", 5},
		{"te", 2},   // Should match test
		{"tea", 2},  // Should match team
		{"toa", 3},  // Should match toast
		{"tac", 5},  // Should match taco
		{"tack", 5}, // Should match tackle
		{"t", 3},    // Should match the least recent split
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

func TestOverlappingPrefixes(t *testing.T) {
	tree := NewTree[byte, int]()

	// Insert strings with complex overlapping patterns
	tree.Insert([]byte("inter"), 1)
	tree.Insert([]byte("internet"), 2)
	tree.Insert([]byte("interview"), 3)
	tree.Insert([]byte("interrupt"), 4)
	tree.Insert([]byte("internal"), 5)

	// fmt.Println("Overlapping prefixes tree:")
	// fmt.Println(tree.String())

	// Test LCP matches
	testCases := []struct {
		input    string
		expected int
	}{
		{"inter", 1},
		{"internet", 2},
		{"interview", 3},
		{"interrupt", 4},
		{"internal", 5},
		{"intern", 5},  // Should match inter
		{"interna", 5}, // Should match internal
		{"interv", 3},  // Should match interview
		{"interru", 4}, // Should match interrupt
		{"interne", 2}, // Should match internet
		{"inte", 1},    // Should match inter
		{"int", 1},     // Should match inter
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

func TestEmptyAndSingleCharacterStrings(t *testing.T) {
	tree := NewTree[byte, int]()

	// Test edge cases with empty and single character strings
	tree.Insert([]byte(""), 0)
	tree.Insert([]byte("a"), 1)
	tree.Insert([]byte("aa"), 2)
	tree.Insert([]byte("aaa"), 3)
	tree.Insert([]byte("b"), 4)
	tree.Insert([]byte("bb"), 5)

	// fmt.Println("Empty and single character tree:")
	// fmt.Println(tree.String())

	// Test LCP matches
	testCases := []struct {
		input    string
		expected int
	}{
		{"", 0},     // Empty string should match the empty value
		{"a", 1},    // Single character
		{"aa", 2},   // Double character
		{"aaa", 3},  // Triple character
		{"aaaa", 3}, // Should match aaa
		{"b", 4},    // Single character b
		{"bb", 5},   // Double character bb
		{"bbb", 5},  // Should match bb
		{"c", 0},    // No match, should return zero value
		{"ab", 1},   // No match, should return zero value
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

func TestVeryLongStrings(t *testing.T) {
	tree := NewTree[byte, int]()

	// Test with very long strings to test performance and edge cases
	longStr1 := []byte("thisisaverylongstringthatshouldtesttheprefixmatchingcapabilitiesoftheradixtreeimplementation")
	longStr2 := []byte("thisisaverylongstringthatshouldtesttheprefixmatchingcapabilitiesoftheradixtreeimplementationwithextension")
	longStr3 := []byte("thisisadifferentlongstring")

	tree.Insert(longStr1, 1)
	tree.Insert(longStr2, 2)
	tree.Insert(longStr3, 3)

	// fmt.Println("Very long strings tree:")
	// fmt.Println(tree.String())

	// Test LCP matches
	testCases := []struct {
		input    string
		expected int
	}{
		{string(longStr1), 1},
		{string(longStr2), 2},
		{string(longStr3), 3},
		{"thisisaverylongstringthatshouldtesttheprefixmatchingcapabilitiesoftheradixtreeimplementation", 1},
		{"thisisaverylongstringthatshouldtesttheprefixmatchingcapabilitiesoftheradixtreeimplementationwith", 2},
		{"thisisaverylongstringthatshouldtesttheprefixmatchingcapabilitiesoftheradixtreeimplementationwithextension", 2},
		{"thisisadifferentlongstrin", 3},
		{"thisisadifferentlongstringx", 3},
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

func TestSpecialCharactersAndUnicode(t *testing.T) {
	tree := NewTree[byte, int]()

	// Test with special characters and unicode
	tree.Insert([]byte("hello世界"), 1)
	tree.Insert([]byte("hello世界！"), 2)
	tree.Insert([]byte("hello😊"), 3)
	tree.Insert([]byte("hello\nworld"), 4)
	tree.Insert([]byte("hello\tworld"), 5)

	// fmt.Println("Special characters tree:")
	// fmt.Println(tree.String())

	// Test LCP matches
	testCases := []struct {
		input    string
		expected int
	}{
		{"hello世界", 1},
		{"hello世界！", 2},
		{"hello😊", 3},
		{"hello\nworld", 4},
		{"hello\tworld", 5},
		{"hello世", 1},  // Should match hello世界
		{"hello😊x", 3}, // Should match hello😊
		{"hello\n", 4}, // Should match hello\nworld
		{"hello\t", 5}, // Should match hello\tworld
		{"hello", 3},   // Should match hello😊
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}

	match, _, _ := tree.LongestCommonPrefixMatch([]byte("hello世"))
	if string(match) != "hello世" {
		t.Errorf("LCP(%q) = %v, expected %q", "hello世", string(match), "hello世")
	}
}

func TestInsertAfterLCP(t *testing.T) {
	tree := NewTree[byte, int]()

	// Insert initial strings
	tree.Insert([]byte("test"), 1)
	tree.Insert([]byte("testing"), 2)

	// Test LCP before adding more
	_, result, _ := tree.LongestCommonPrefixMatch([]byte("test"))
	if result == nil || *result != 1 {
		t.Errorf("LCP(test) = %v, expected 1", result)
	}

	// Add more strings that should split existing nodes
	tree.Insert([]byte("tester"), 3)
	tree.Insert([]byte("tests"), 4)

	// fmt.Println("Insert after LCP tree:")
	// fmt.Println(tree.String())

	// Test that LCP still works correctly after new insertions
	testCases := []struct {
		input    string
		expected int
	}{
		{"test", 1},
		{"testing", 2},
		{"tester", 3},
		{"tests", 4},
		{"testx", 1},   // Should match test
		{"testin", 2},  // Should match testing
		{"testerr", 3}, // Should match tester
		{"testsa", 4},  // Should match tests
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

func TestRemoveNode(t *testing.T) {
	tree := NewTree[byte, int]()

	// Test 1: Remove leaf node
	node1 := tree.Insert([]byte("hello"), 1)
	tree.Insert([]byte("world"), 2)

	// Verify initial state
	if len(tree.Root.Children) != 2 {
		t.Errorf("Expected 2 children, got %d", len(tree.Root.Children))
	}

	// Remove one node
	tree.RemoveNode(node1)
	if len(tree.Root.Children) != 1 {
		t.Errorf("Expected 1 child after removal, got %d", len(tree.Root.Children))
	}

	// Verify the remaining node still works
	_, result, _ := tree.LongestCommonPrefixMatch([]byte("world"))
	if result == nil || *result != 2 {
		t.Errorf("Expected 2, got %v", result)
	}
	_, result, _ = tree.LongestCommonPrefixMatch([]byte("hello"))
	if result != nil {
		t.Errorf("Expected nil, got %v", *result)
	}
}

func TestRemoveNodeWithParentCleanup(t *testing.T) {
	tree := NewTree[byte, int]()

	// Insert strings that create parent-child relationships
	tree.Insert([]byte("hello"), 1)
	node2 := tree.Insert([]byte("help"), 2)
	node3 := tree.Insert([]byte("helper"), 3)

	// Verify initial structure
	if len(tree.Root.Children) != 1 {
		t.Errorf("Expected 1 child at root, got %d", len(tree.Root.Children))
	}

	//fmt.Println(tree.String())
	// Remove the leaf node ("helper")
	tree.RemoveNode(node3)
	// fmt.Println(tree.String())
	_, result, _ := tree.LongestCommonPrefixMatch([]byte("help"))
	if result == nil || *result != 2 {
		t.Errorf("Expected 2, got %v", result)
	}

	// Remove the middle node ("help"), which should has no effect
	tree.RemoveNode(node2)
	// fmt.Println(tree.String())
	// Verify only "hello" remains
	_, result, _ = tree.LongestCommonPrefixMatch([]byte("hello"))
	if result == nil || *result != 1 {
		t.Errorf("Expected 1, got %v", result)
	}

	// Verify no children remain at root
	if len(tree.Root.Children) != 1 {
		t.Errorf("Expected 1 child at root, got %d", len(tree.Root.Children))
	}
}

func TestRemoveRootNode(t *testing.T) {
	tree := NewTree[byte, int]()

	// Try to remove root node (should not panic)
	tree.RemoveNode(tree.Root)

	// Root should still exist
	if tree.Root == nil {
		t.Error("Root should not be nil after attempted removal")
	}
}

func TestRemoveNonexistentNode(t *testing.T) {
	tree := NewTree[byte, int]()

	x := 42
	// Create a node not in the tree
	externalNode := NewNode([]byte("external"), &x)

	// Try to remove it (should not panic)
	tree.RemoveNode(externalNode)

	// Tree should remain unchanged
	if len(tree.Root.Children) != 0 {
		t.Errorf("Expected 0 children, got %d", len(tree.Root.Children))
	}
}

func TestRemoveNodeComplexTree(t *testing.T) {
	tree := NewTree[byte, int]()

	// Build a complex tree structure
	tree.Insert([]byte("romanus"), 1)
	node2 := tree.Insert([]byte("romane"), 2)
	node3 := tree.Insert([]byte("romulus"), 3)
	tree.Insert([]byte("rubens"), 4)
	tree.Insert([]byte("ruber"), 5)
	tree.Insert([]byte("rubicon"), 6)
	node7 := tree.Insert([]byte("rubicundus"), 7)

	// fmt.Println(tree.String())
	// Remove nodes and verify tree integrity
	tree.RemoveNode(node2)
	tree.RemoveNode(node3)
	tree.RemoveNode(node7)
	// fmt.Println(tree.String())

	// Verify remaining nodes still work
	testCases := []struct {
		input    string
		expected int
	}{
		{"romanus", 1},
		{"rubens", 4},
		{"ruber", 5},
		{"rubicon", 6},
		{"roma", 1},       // Should match romanus
		{"rub", 6},        // Should match rubicon
		{"rubicundus", 6}, // Should match rubicon
		{"rube", 5},       // Should match ruber
	}

	for _, tc := range testCases {
		_, result, _ := tree.LongestCommonPrefixMatch([]byte(tc.input))
		if result == nil && tc.expected != 0 {
			t.Errorf("LCP(%q) = nil, expected %d", tc.input, tc.expected)
		} else if result != nil && *result != tc.expected {
			t.Errorf("LCP(%q) = %v, expected %d", tc.input, *result, tc.expected)
		}
	}
}

func TestRemoveIntermidiateNodeCleanup(t *testing.T) {
	tree := NewTree[byte, int]()

	// Insert strings that create a chain
	tree.Insert([]byte("a"), 1)
	node2 := tree.Insert([]byte("ab"), 2)
	node3 := tree.Insert([]byte("abc"), 3)

	// Remove the leaf node
	tree.RemoveNode(node3)

	// Verify parent still exists
	_, result, _ := tree.LongestCommonPrefixMatch([]byte("ab"))
	if result == nil || *result != 2 {
		t.Errorf("Expected 2, got %v", result)
	}

	// Remove the middle node
	tree.Insert([]byte("abc"), 3)
	tree.RemoveNode(node2)

	// Verify only "abc" remains
	_, result, _ = tree.LongestCommonPrefixMatch([]byte("ab"))
	if result == nil || *result != 3 {
		t.Errorf("Expected 3, got %v", *result)
	}
}
