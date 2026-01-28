package lradix

import (
	"fmt"
	"strings"
	"sync"
)

type Match[T any] struct {
	MatchLength int
	Value       *T
	Exact       bool
}

func NewMatch[T any](l int, v *T, exact bool) Match[T] {
	return Match[T]{
		MatchLength: l,
		Value:       v,
		Exact:       exact,
	}
}

// ConcurrentNode represents a thread-safe node in the radix tree.
// It contains a read-write mutex for concurrent access, text fragment of type K,
// associated value of type T, and child nodes. The mutex ensures thread-safe
// operations on the node's data.
type ConcurrentNode[K comparable, T any] struct {
	sync.RWMutex
	Text     []K                         // Text fragment for this node (of comparable type K)
	Val      *T                          // Value associated with this node (intermediate will save a random child's value)
	End      bool                        // Whether this node is a intermediate node
	Deleted  bool                        // Whether this node has been deleted
	Children map[K]*ConcurrentNode[K, T] // Child nodes indexed by first character (key type K)
	Parent   *ConcurrentNode[K, T]       // Parent node for tree traversal
}

// GetChild retrieves a child node by its first character (type K).
// Returns the child node and a boolean indicating if it was found.
// Note: the caller is responsible for acquiring the necessary locks before calling this method.
func (n *ConcurrentNode[K, T]) GetChild(head K) (*ConcurrentNode[K, T], bool) {
	child, ok := n.Children[head]
	return child, ok
}

// AddChild adds a child node to this node.
// It automatically sets the parent pointer and indexes the child by its first character (type K).
// Note: This method must hold locks of both parent and child nodes to ensure thread safety.
// The caller is responsible for acquiring the necessary locks before calling this method.
func (n *ConcurrentNode[K, T]) AddChild(node *ConcurrentNode[K, T]) {
	if len(node.Text) == 0 {
		return
	}
	if n.Children == nil {
		n.Children = map[K]*ConcurrentNode[K, T]{}
	}
	node.Parent = n
	n.Children[node.Text[0]] = node
}

// NewConcurrentNode creates a new concurrent node with the given text (type K), value (type T), and end flag.
// The node is initialized with an empty children map and is ready for concurrent operations.
// The end flag determines whether this node represents the end of a complete key.
func NewConcurrentNode[K comparable, T any](text []K, val *T, end bool) *ConcurrentNode[K, T] {
	return &ConcurrentNode[K, T]{
		Text:     text,
		Val:      val,
		End:      end,
		Children: map[K]*ConcurrentNode[K, T]{},
	}
}

// ConcurrentTree represents a thread-safe radix tree data structure.
// It provides efficient concurrent insertion and longest common prefix matching operations
// for keys of type K and values of type T. All operations are thread-safe and use
// fine-grained locking to maximize concurrency.
type ConcurrentTree[K comparable, T any] struct {
	Root *ConcurrentNode[K, T] // Root node of the tree
}

// NewConcurrentTree creates a new empty concurrent radix tree with keys of type K and values of type T.
// The tree is initialized with a root node and is ready for concurrent operations.
func NewConcurrentTree[K comparable, T any]() *ConcurrentTree[K, T] {
	return &ConcurrentTree[K, T]{
		Root: NewConcurrentNode[K, T]([]K{}, nil, false),
	}
}

// Insert inserts a key-value pair into the tree in a thread-safe manner.
// The key is represented as a slice of type K, and the value is of type T.
// If the key already exists, it will be overwritten.
// This method uses fine-grained locking to ensure thread safety while maximizing concurrency.
// Returns the newly created node or nil if insertion failed.
func (t *ConcurrentTree[K, T]) Insert(str []K, val T) *ConcurrentNode[K, T] {
	if len(str) == 0 {
		return nil
	}
	mark := t.Root
	index := 0
	for index < len(str) {
		cur := mark
		char := str[index]
		cur.Lock() // ===🟧===
		next, ok := cur.GetChild(char)
		if !ok {
			// no match, add new node to current children
			newNode := NewConcurrentNode(str[index:], &val, false)
			cur.AddChild(newNode)
			cur.Unlock() // ===🟠===
			return newNode
		}
		next.Lock() // ===🟦===
		sharedPrefix := longestPrefix(next.Text, str[index:])
		if sharedPrefix < len(next.Text) {
			// partial match, split node
			// use this insert val as common node val, because it is most recent
			commonNode := NewConcurrentNode(next.Text[:sharedPrefix], &val, false)
			cur.AddChild(commonNode)
			if cur.Parent != nil {
				// if not root, update parent val
				cur.Val = &val
			}
			next.Text = next.Text[sharedPrefix:]
			commonNode.AddChild(next)
			if index+sharedPrefix < len(str) {
				// commonNode is an intermediate node
				newNode := NewConcurrentNode(str[index+sharedPrefix:], &val, true)
				commonNode.Val = nil
				commonNode.AddChild(newNode)
				cur.Unlock()  // ===🟠===
				next.Unlock() // ===🔵===
				return newNode
			} else {
				cur.Unlock()  // ===🟠===
				next.Unlock() // ===🔵===
				return commonNode
			}
		}
		cur.Unlock()  // ===🟠===
		next.Unlock() // ===🔵===
		// full match, move to next node
		index += sharedPrefix
		mark = next
	}
	mark.Lock()
	mark.Val = &val
	mark.End = true
	mark.Unlock()
	return mark
}

// LongestCommonPrefixMatch finds the longest prefix in the tree that matches the given key.
// It returns three values: the longest common prefix (slice of type K), associated value (pointer to type T),
// and a boolean indicating whether it is an exact match. This operation is thread-safe and uses
// read locks to allow concurrent reads while ensuring data consistency.
func (t *ConcurrentTree[K, T]) LongestCommonPrefixMatch(str []K) ([]K, *T, bool) {
	commonPrefix := []K{}
	cur := t.Root
	var val *T
	var isIntermediate bool
	index := 0
	for index < len(str) {
		// from root to cur, all text connected equal to str[:index]
		char := str[index]

		// find next node. next.Text start with str[index]
		cur.RLock()
		next, ok := cur.GetChild(char)
		cur.RUnlock()

		if !ok {
			// no more match, stop
			return commonPrefix, val, false
		}
		cur = next
		next.RLock()
		nextText := next.Text
		if next.Val != nil {
			// val is the last matched node's value
			val = next.Val
			isIntermediate = false
		} else {
			isIntermediate = true
		}
		next.RUnlock()
		sharedPrefix := longestPrefix(nextText, str[index:])
		commonPrefix = append(commonPrefix, nextText[:sharedPrefix]...)
		if sharedPrefix < len(nextText) {
			// partial match, stop
			return commonPrefix, val, false
		}
		// full match, try to find next node
		index += sharedPrefix
	}
	if isIntermediate {
		childVal := dfsNodeForValue(cur)
		if childVal != nil {
			val = childVal
		}
		return commonPrefix, val, false
	} else {
		return commonPrefix, val, true
	}

}

func dfsNodeForValue[K comparable, T any](root *ConcurrentNode[K, T]) *T {
	root.RLock()
	if root.Val != nil {
		val := root.Val
		root.RUnlock()
		return val
	}

	// Snapshot children to avoid holding lock during recursion
	children := make([]*ConcurrentNode[K, T], 0, len(root.Children))
	for _, child := range root.Children {
		children = append(children, child)
	}
	root.RUnlock()

	for _, child := range children {
		val := dfsNodeForValue(child)
		if val != nil {
			return val
		}
	}
	return nil
}

func (t *ConcurrentTree[K, T]) MultiLongestCommonPrefixMatch(str []K) []Match[T] {
	candidates := []Match[T]{}
	cur := t.Root
	index := 0
	for index < len(str) {
		char := str[index]
		// no match，stop at current node
		cur.RLock()
		next, ok := cur.GetChild(char)
		val := cur.Val
		if val != nil {
			candidates = append(candidates, NewMatch(index, val, false))
		}
		cur.RUnlock()
		if !ok {
			break
		}
		for _, child := range cur.Children {
			child.RLock()
			if child.Val != nil {
				candidates = append(candidates, NewMatch(index, child.Val, false))
			}
			child.RUnlock()
		}
		cur = next
		next.RLock()
		matchText := next.Text
		next.RUnlock()
		sharedPrefixLength := longestPrefix(matchText, str[index:])
		if sharedPrefixLength < len(matchText) {
			// partial match, stop
			break
		}
		// full match, move to next node
		index += sharedPrefixLength
	}
	cur.RLock()
	defer cur.RUnlock()
	if cur.Val != nil {
		candidates = append(candidates, NewMatch(index, cur.Val, cur.End))
	}
	for _, child := range cur.Children {
		child.RLock()
		if child.Val != nil {
			candidates = append(candidates, NewMatch(index, child.Val, false))
		}
		child.RUnlock()
	}
	return candidates
}

// RemoveNode removes a node from the tree in a thread-safe manner.
// Only leaf nodes (nodes without children) can be removed.
// When a leaf node is removed, its parent may also be removed if it becomes
// an intermediate node with no children and doesn't represent a complete key.
// This method uses proper locking to ensure thread safety during the removal process.
func (t *ConcurrentTree[K, T]) RemoveNode(node *ConcurrentNode[K, T]) {
	node.Lock()
	defer node.Unlock()
	if node.Parent == nil {
		// root node can't be removed
		return
	}
	node.Val = nil
	node.End = false
	node.Deleted = true
}

// String returns a string representation of the tree structure.
// Useful for debugging and visualization. Handles different key types appropriately.
// This operation is thread-safe and uses read locks to ensure consistent output.
func (t *ConcurrentTree[K, T]) String() string {
	var result strings.Builder
	printConcurrentNode(t.Root, "", &result)
	return result.String()
}

// printConcurrentNode recursively prints a node and its children for the String() method.
// Handles different key types (K) for proper string representation. This operation
// is thread-safe and uses read locks to ensure consistent output.
func printConcurrentNode[K comparable, T any](node *ConcurrentNode[K, T], prefix string, result *strings.Builder) {
	if node == nil {
		return
	}
	node.RLock()
	defer node.RUnlock()

	var displayText string
	if len(node.Text) == 0 {
		displayText = "ROOT"
	} else {
		switch v := any(node.Text).(type) {
		case []byte:
			displayText = string(v)
		case []rune:
			displayText = string(v)
		default:
			displayText = fmt.Sprintf("%v", node.Text)
		}
	}

	result.WriteString(prefix)
	result.WriteString("└──")
	result.WriteString(displayText)

	result.WriteString(" (val: ")
	if node.Val == nil {
		result.WriteString("nil")
	} else {
		result.WriteString(fmt.Sprintf("%v", *node.Val))
	}
	result.WriteString(")")
	result.WriteString("\n")

	newPrefix := prefix + "   "
	for _, child := range node.Children {
		printConcurrentNode(child, newPrefix, result)
	}
}
