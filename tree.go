// Package lradix implements a radix tree (prefix tree) that supports longest common prefix matching.
//
// This package provides a generic radix tree implementation that can store any type of value
// associated with keys of any comparable type. The tree automatically handles prefix splitting and merging
// to optimize storage and provide efficient longest prefix matching.
//
// Basic usage:
//
//	tree := lradix.NewTree[byte, int]()
//	tree.Insert([]byte("hello"), 1)
//	tree.Insert([]byte("world"), 2)
//
//	prefix, value, exact := tree.LongestCommonPrefixMatch([]byte("hello world"))
//	// prefix will be []byte("hello"), value will be 1, exact will be false
package lradix

import (
	"fmt"
	"strings"
)

// Node represents a node in the radix tree.
// It contains the text fragment of type K, associated value of type T, and child nodes.
type Node[K comparable, T any] struct {
	Text     []K               // Text fragment for this node (of comparable type K)
	Val      *T                // Value associated with this node (nil for intermediate nodes, of type T)
	End      bool              // Whether this node represents the end of a complete key
	Children map[K]*Node[K, T] // Child nodes indexed by first character (key type K)
	Parent   *Node[K, T]       // Parent node for tree traversal
}

// NewNode creates a new leaf node with the given text (type K) and value (type T).
// A leaf node represents the end of a complete key.
func NewNode[K comparable, T any](text []K, val *T) *Node[K, T] {
	return &Node[K, T]{
		Text:     text,
		Val:      val,
		End:      true,
		Children: map[K]*Node[K, T]{},
	}
}

// NewIntermediateNode creates a new intermediate node with the given text (type K) and value (type T).
// An intermediate node does not represent the end of a complete key.
func NewIntermediateNode[K comparable, T any](text []K, val *T) *Node[K, T] {
	return &Node[K, T]{
		Text:     text,
		Val:      val,
		End:      false,
		Children: map[K]*Node[K, T]{},
	}
}

// AddChild adds a child node to this node.
// It automatically sets the parent pointer and indexes the child by its first character (type K).
func (n *Node[K, T]) AddChild(node *Node[K, T]) {
	if len(node.Text) == 0 {
		return
	}
	if n.Children == nil {
		n.Children = map[K]*Node[K, T]{}
	}
	node.Parent = n
	n.Children[node.Text[0]] = node
}

// GetChild retrieves a child node by its first character (type K).
// Returns the child node and a boolean indicating if it was found.
func (n *Node[K, T]) GetChild(head K) (*Node[K, T], bool) {
	child, ok := n.Children[head]
	return child, ok
}

// Tree represents a radix tree data structure.
// It provides efficient insertion and longest common prefix matching operations for keys of type K and values of type T.
type Tree[K comparable, T any] struct {
	Root *Node[K, T] // Root node of the tree
}

// NewTree creates a new empty radix tree with keys of type K and values of type T.
func NewTree[K comparable, T any]() *Tree[K, T] {
	return &Tree[K, T]{
		Root: &Node[K, T]{
			Text:     []K{},
			Children: map[K]*Node[K, T]{},
		},
	}
}

// Insert inserts a key-value pair into the tree.
// The key is represented as a slice of type K, and the value is of type T.
// If the key already exists, it will be overwritten.
// Returns the newly created node or nil if insertion failed.
func (t *Tree[K, T]) Insert(str []K, val T) *Node[K, T] {
	if len(str) == 0 {
		return nil
	}
	mark := t.Root
	index := 0
	for index < len(str) {
		cur := mark
		char := str[index]
		next, ok := cur.GetChild(char)
		if !ok {
			// no match, add new node to current children
			newNode := NewNode(str[index:], &val)
			cur.AddChild(newNode)
			return newNode
		}
		sharedPrefix := longestPrefix(next.Text, str[index:])
		if sharedPrefix < len(next.Text) {
			// partial match, split node
			// use this insert val as common node val, because it is most recent
			commonNode := NewIntermediateNode(next.Text[:sharedPrefix], &val)
			cur.AddChild(commonNode)
			if cur.Parent != nil {
				// if not root, update parent val
				cur.Val = &val
			}
			next.Text = next.Text[sharedPrefix:]
			commonNode.AddChild(next)
			if index+sharedPrefix < len(str) {
				newNode := NewNode(str[index+sharedPrefix:], &val)
				commonNode.AddChild(newNode)
				return newNode
			} else {
				commonNode.End = true
				return commonNode
			}
		}
		// full match, move to next node
		index += sharedPrefix
		mark = next
	}
	mark.Val = &val
	mark.End = true
	return mark
}

// LongestCommonPrefixMatch finds the longest prefix in the tree that matches the given key.
// It returns three values: the longest common prefix (slice of type K), associated value (pointer to type T),
// and a boolean indicating whether it is an exact match.
// This is the core operation for prefix-based routing and matching.
func (t *Tree[K, T]) LongestCommonPrefixMatch(str []K) ([]K, *T, bool) {
	commonPrefix := []K{}
	mark := t.Root
	index := 0
	for index < len(str) {
		cur := mark
		char := str[index]
		// no match，stop at current node
		next, ok := cur.GetChild(char)
		if !ok {
			return commonPrefix, mark.Val, false
		}
		mark = next
		sharedPrefix := longestPrefix(next.Text, str[index:])
		commonPrefix = append(commonPrefix, next.Text[:sharedPrefix]...)
		if sharedPrefix < len(next.Text) {
			// partial match, stop
			return commonPrefix, mark.Val, false
		}
		// full match, move to next node
		index += sharedPrefix
	}
	return commonPrefix, mark.Val, mark.End
}

// RemoveNode removes a node from the tree.
// Only leaf nodes (nodes without children) can be removed.
// When a leaf node is removed, its parent may also be removed if it becomes
// an intermediate node with no children and doesn't represent a complete key.
// The node parameter is of type Node[K, T] with the same generic types as the tree.
func (t *Tree[K, T]) RemoveNode(node *Node[K, T]) {
	if len(node.Children) > 0 {
		for _, v := range node.Children {
			node.Val = v.Val
		}
		node.End = false
		return
	}
	parent := node.Parent
	node.Parent = nil
	if parent == nil {
		// root node can't be removed
		return
	}

	delete(parent.Children, node.Text[0])
	if len(parent.Children) == 0 && !parent.End {
		t.RemoveNode(parent)
	} else {
		if parent.Parent == nil {
			// root node needs not to be updated
			return
		}
		for _, v := range parent.Children {
			parent.Val = v.Val
			break
		}
	}
}

// String returns a string representation of the tree structure.
// Useful for debugging and visualization. Handles different key types appropriately.
func (t *Tree[K, T]) String() string {
	var result strings.Builder
	printNode(t.Root, "", &result)
	return result.String()
}

// printNode recursively prints a node and its children for the String() method.
// Handles different key types (K) for proper string representation.
func printNode[K comparable, T any](node *Node[K, T], prefix string, result *strings.Builder) {
	if node == nil {
		return
	}

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
		printNode(child, newPrefix, result)
	}
}

// longestPrefix returns the length of the longest common prefix between two slices of type K.
// This is a helper function used for prefix matching and node splitting.
func longestPrefix[K comparable](a, b []K) int {
	i := 0
	for ; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return i
}
