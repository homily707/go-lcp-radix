package lradix

import (
	"sync"
	"testing"
)

func boolPtr(v bool) *bool {
	return &v
}

func assertConcurrentMatch(
	t *testing.T,
	tree *ConcurrentTree[rune, int],
	input, wantPrefix string,
	wantVal *int,
	wantExact *bool,
) {
	t.Helper()

	_, gotPrefix, gotVal, gotExact := tree.LongestCommonPrefixMatch([]rune(input))
	if string(gotPrefix) != wantPrefix {
		t.Fatalf("input=%q prefix=%q, want %q", input, string(gotPrefix), wantPrefix)
	}

	if wantExact != nil && gotExact != *wantExact {
		t.Fatalf("input=%q exact=%v, want %v", input, gotExact, *wantExact)
	}

	if wantVal == nil {
		if gotVal != nil {
			t.Fatalf("input=%q value=%v, want nil", input, *gotVal)
		}
		return
	}

	if gotVal == nil {
		t.Fatalf("input=%q value=nil, want %v", input, *wantVal)
	}
	if *gotVal != *wantVal {
		t.Fatalf("input=%q value=%v, want %v", input, *gotVal, *wantVal)
	}
}

func TestConcurrentTree_InsertAndLCP_HappyPath(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()
	tree.Insert([]rune("hello"), 1)
	tree.Insert([]rune("help"), 2)
	tree.Insert([]rune("helium"), 3)
	tree.Insert([]rune("world"), 4)

	assertConcurrentMatch(t, tree, "help", "help", intPtr(2), boolPtr(true))
	assertConcurrentMatch(t, tree, "hello!", "hello", intPtr(1), boolPtr(false))
	assertConcurrentMatch(t, tree, "helix", "heli", intPtr(3), boolPtr(false))
	assertConcurrentMatch(t, tree, "world!", "world", intPtr(4), boolPtr(false))
	assertConcurrentMatch(t, tree, "zoo", "", nil, boolPtr(false))
}

func TestConcurrentTree_Insert_EmptyKeyIgnored(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()
	if got := tree.Insert([]rune(""), 123); got != nil {
		t.Fatalf("empty key insert should return nil")
	}
	assertConcurrentMatch(t, tree, "anything", "", nil, boolPtr(false))
}

func TestConcurrentTree_MultiLCP_Candidates(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()
	tree.Insert([]rune("hello"), 1)
	tree.Insert([]rune("help"), 2)

	matches := tree.MultiLongestCommonPrefixMatch([]rune("help!"))
	if len(matches) != 3 {
		t.Fatalf("len(matches)=%d, want 3", len(matches))
	}

	if matches[0].Value != nil || matches[0].MatchLength != 0 {
		t.Fatalf("match[0]=%+v, want value=nil length=0", matches[0])
	}
	if matches[1].Value == nil || *matches[1].Value != 2 || matches[1].MatchLength != 3 {
		t.Fatalf("match[1]=%+v, want value=2 length=3", matches[1])
	}
	if matches[2].Value == nil || *matches[2].Value != 2 || matches[2].MatchLength != 4 {
		t.Fatalf("match[2]=%+v, want value=2 length=4", matches[2])
	}
}

func TestConcurrentTree_Remove_Leaf(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()
	tree.Insert([]rune("hello"), 1)
	tree.Insert([]rune("help"), 2)
	helper := tree.Insert([]rune("helper"), 3)

	tree.RemoveNode(helper)

	assertConcurrentMatch(t, tree, "help", "help", intPtr(2), boolPtr(true))
	assertConcurrentMatch(t, tree, "helper", "help", intPtr(2), boolPtr(false))
}

func TestConcurrentTree_Remove_NonLeaf(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()
	tree.Insert([]rune("hello"), 1)
	help := tree.Insert([]rune("help"), 2)
	tree.Insert([]rune("helper"), 3)

	tree.RemoveNode(help)

	if help.End {
		t.Fatalf("help node should become non-terminal after RemoveNode")
	}
	if help.Val == nil || *help.Val != 3 {
		t.Fatalf("help node value=%v, want 3", help.Val)
	}

	assertConcurrentMatch(t, tree, "help", "help", intPtr(3), boolPtr(false))
	assertConcurrentMatch(t, tree, "helper", "helper", intPtr(3), boolPtr(false))
}

func TestConcurrentTree_ConcurrentReadWrite(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()

	type kv struct {
		key string
		val int
	}
	inserts := []kv{
		{key: "alpha", val: 1},
		{key: "bravo", val: 2},
		{key: "charlie", val: 3},
		{key: "delta", val: 4},
		{key: "echo", val: 5},
	}

	lookups := []string{"alpha!", "bravo!", "charlie!", "unknown"}

	var wg sync.WaitGroup
	for _, in := range inserts {
		wg.Add(1)
		go func(item kv) {
			defer wg.Done()
			tree.Insert([]rune(item.key), item.val)
		}(in)
	}
	for _, query := range lookups {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			tree.LongestCommonPrefixMatch([]rune(s))
		}(query)
	}
	wg.Wait()

	for _, in := range inserts {
		assertConcurrentMatch(t, tree, in.key, in.key, intPtr(in.val), nil)
	}
}

func TestConcurrentTree_Remove_RootAndExternalNodeNoop(t *testing.T) {
	tree := NewConcurrentTree[rune, int]()
	tree.Insert([]rune("hello"), 1)

	tree.RemoveNode(tree.Root)
	assertConcurrentMatch(t, tree, "hello", "hello", intPtr(1), boolPtr(false))

	x := 99
	external := NewConcurrentNode([]rune("ghost"), &x, true)
	tree.RemoveNode(external)
	assertConcurrentMatch(t, tree, "hello", "hello", intPtr(1), boolPtr(false))
}
