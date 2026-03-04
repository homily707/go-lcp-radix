package lradix

import "testing"

func intPtr(v int) *int {
	return &v
}

func assertTreeMatch(t *testing.T, tree *Tree[byte, int], input, wantPrefix string, wantVal *int, wantExact bool) {
	t.Helper()

	gotPrefix, gotVal, gotExact := tree.LongestCommonPrefixMatch([]byte(input))
	if string(gotPrefix) != wantPrefix {
		t.Fatalf("input=%q prefix=%q, want %q", input, string(gotPrefix), wantPrefix)
	}
	if gotExact != wantExact {
		t.Fatalf("input=%q exact=%v, want %v", input, gotExact, wantExact)
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

func TestTree_InsertAndLCP_HappyPath(t *testing.T) {
	tree := NewTree[byte, int]()
	tree.Insert([]byte("hello"), 1)
	tree.Insert([]byte("help"), 2)
	tree.Insert([]byte("helium"), 3)
	tree.Insert([]byte("world"), 4)
	tree.Insert([]byte("hello"), 9)

	tests := []struct {
		name       string
		input      string
		wantPrefix string
		wantVal    *int
		wantExact  bool
	}{
		{
			name:       "exact after overwrite",
			input:      "hello",
			wantPrefix: "hello",
			wantVal:    intPtr(9),
			wantExact:  true,
		},
		{
			name:       "longest prefix",
			input:      "hello-world",
			wantPrefix: "hello",
			wantVal:    intPtr(9),
			wantExact:  false,
		},
		{
			name:       "partial match inside branch",
			input:      "helix",
			wantPrefix: "heli",
			wantVal:    intPtr(3),
			wantExact:  false,
		},
		{
			name:       "other branch",
			input:      "world!",
			wantPrefix: "world",
			wantVal:    intPtr(4),
			wantExact:  false,
		},
		{
			name:       "no match",
			input:      "zoo",
			wantPrefix: "",
			wantVal:    nil,
			wantExact:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertTreeMatch(t, tree, tc.input, tc.wantPrefix, tc.wantVal, tc.wantExact)
		})
	}
}

func TestTree_Insert_EmptyKeyIgnored(t *testing.T) {
	tree := NewTree[byte, int]()
	if got := tree.Insert([]byte(""), 123); got != nil {
		t.Fatalf("empty key insert should return nil")
	}
	assertTreeMatch(t, tree, "anything", "", nil, false)
}

func TestTree_Remove_Leaf(t *testing.T) {
	tree := NewTree[byte, int]()
	tree.Insert([]byte("help"), 1)
	helper := tree.Insert([]byte("helper"), 2)

	tree.RemoveNode(helper)

	assertTreeMatch(t, tree, "help", "help", intPtr(1), true)
	assertTreeMatch(t, tree, "helper", "help", intPtr(1), false)
}

func TestTree_Remove_NonLeaf(t *testing.T) {
	tree := NewTree[byte, int]()
	help := tree.Insert([]byte("help"), 1)
	tree.Insert([]byte("helper"), 2)

	tree.RemoveNode(help)

	if help.End {
		t.Fatalf("help node should become non-terminal after RemoveNode")
	}
	if help.Val == nil || *help.Val != 2 {
		t.Fatalf("help node value=%v, want 2", help.Val)
	}

	assertTreeMatch(t, tree, "help", "help", intPtr(2), false)
	assertTreeMatch(t, tree, "helper", "helper", intPtr(2), true)
}

func TestTree_Remove_RecursiveCleanup(t *testing.T) {
	tree := NewTree[byte, int]()
	node := tree.Insert([]byte("abc"), 1)

	tree.RemoveNode(node)

	if len(tree.Root.Children) != 0 {
		t.Fatalf("root children=%d, want 0", len(tree.Root.Children))
	}
	assertTreeMatch(t, tree, "abc", "", nil, false)
}

func TestTree_Remove_RootAndExternalNodeNoop(t *testing.T) {
	tree := NewTree[byte, int]()
	tree.Insert([]byte("hello"), 1)

	tree.RemoveNode(tree.Root)
	if tree.Root == nil {
		t.Fatalf("root should not be nil")
	}
	if len(tree.Root.Children) != 1 {
		t.Fatalf("root children=%d, want 1", len(tree.Root.Children))
	}

	x := 7
	external := NewNode([]byte("ghost"), &x)
	tree.RemoveNode(external)
	if len(tree.Root.Children) != 1 {
		t.Fatalf("tree should stay unchanged, root children=%d", len(tree.Root.Children))
	}
}
