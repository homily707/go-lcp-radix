// Package lradix implements a radix tree (prefix tree) that supports longest common prefix matching.
//
// This package provides a generic radix tree implementation that can store any type of value
// associated with string keys. The tree automatically handles prefix splitting and merging
// to optimize storage and provide efficient longest prefix matching.
//
// Basic usage:
//
//	tree := lradix.NewTree[int]()
//	tree.Insert([]byte("hello"), 1)
//	tree.Insert([]byte("world"), 2)
//
//	result := tree.LongestCommonPrefixMatch([]byte("hello world"))
//	// result will be 1
package lradix

import (
	"fmt"
	"sort"
	"strings"
)

// Node represents a node in the radix tree.
// It contains the text fragment, associated value, and child nodes.
type Node[T any] struct {
	Text     []byte            // Text fragment for this node
	Val      *T                // Value associated with this node (nil for intermediate nodes)
	End      bool              // Whether this node represents the end of a complete key
	Children map[byte]*Node[T] // Child nodes indexed by first character
	Parent   *Node[T]          // Parent node for tree traversal
}

// NewNode creates a new leaf node with the given text and value.
// A leaf node represents the end of a complete key.
func NewNode[T any](text []byte, val *T) *Node[T] {
	return &Node[T]{
		Text:     text,
		Val:      val,
		End:      true,
		Children: map[byte]*Node[T]{},
	}
}

// NewIntermediateNode creates a new intermediate node with the given text and value.
// An intermediate node does not represent the end of a complete key.
func NewIntermediateNode[T any](text []byte, val *T) *Node[T] {
	return &Node[T]{
		Text:     text,
		Val:      val,
		End:      false,
		Children: map[byte]*Node[T]{},
	}
}

// AddChild adds a child node to this node.
// It automatically sets the parent pointer and indexes the child by its first character.
func (n *Node[T]) AddChild(node *Node[T]) {
	if n.Children == nil {
		n.Children = map[byte]*Node[T]{}
	}
	node.Parent = n
	if len(node.Text) == 0 {
		n.Children[byte(0)] = node
		return
	}
	n.Children[node.Text[0]] = node
}

// GetChild retrieves a child node by its first character.
// Returns the child node and a boolean indicating if it was found.
func (n *Node[T]) GetChild(char byte) (*Node[T], bool) {
	child, ok := n.Children[char]
	return child, ok
}

// Tree represents a radix tree data structure.
// It provides efficient insertion and longest common prefix matching operations.
type Tree[T any] struct {
	Root *Node[T] // Root node of the tree
}

// NewTree creates a new empty radix tree.
func NewTree[T any]() *Tree[T] {
	return &Tree[T]{
		Root: &Node[T]{
			Text:     []byte{},
			Children: map[byte]*Node[T]{},
		},
	}
}

// Insert inserts a key-value pair into the tree.
// The key is represented as a byte slice, and the value can be of any type.
// If the key already exists, it will be overwritten.
// Returns the newly created node or nil if insertion failed.
func (t *Tree[T]) Insert(str []byte, val T) *Node[T] {
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
			next.Text = next.Text[sharedPrefix:]
			newNode := NewNode(str[index+sharedPrefix:], &val)
			commonNode.AddChild(next)
			commonNode.AddChild(newNode)
			cur.AddChild(commonNode)
			return newNode
		}
		// full match, move to next node
		index += sharedPrefix
		mark = next
	}
	mark.Val = &val
	mark.End = true
	return mark
}

// LongestCommonPrefixMatch finds the longest prefix in the tree that matches the given string.
// It returns the longest common prefix and the value associated with the longest matching prefix, or nil if no match is found.
// This is the core operation for prefix-based routing and matching.
func (t *Tree[T]) LongestCommonPrefixMatch(str []byte) ([]byte, *T) {
	commonPrefix := []byte{}
	mark := t.Root
	index := 0
	for index < len(str) {
		cur := mark
		char := str[index]
		// no match，stop at current node
		next, ok := cur.GetChild(char)
		if !ok {
			break
		}
		mark = next
		sharedPrefix := longestPrefix(next.Text, str[index:])
		commonPrefix = append(commonPrefix, next.Text[:sharedPrefix]...)
		if sharedPrefix < len(next.Text) {
			// partial match, stop
			break
		}
		// full match, move to next node
		index += sharedPrefix
	}
	return commonPrefix, mark.Val
}

// RemoveNode removes a node from the tree.
// Only leaf nodes (nodes without children) can be removed.
// When a leaf node is removed, its parent may also be removed if it becomes
// an intermediate node with no children and doesn't represent a complete key.
func (t *Tree[T]) RemoveNode(node *Node[T]) {
	if len(node.Children) > 0 {
		// has children, can't be removed
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
// Useful for debugging and visualization.
func (t *Tree[T]) String() string {
	var result strings.Builder
	t.printNode(t.Root, "", &result)
	return result.String()
}

// printNode recursively prints a node and its children for the String() method.
func (t *Tree[T]) printNode(node *Node[T], prefix string, result *strings.Builder) {
	if node == nil {
		return
	}

	var displayText string
	if len(node.Text) == 0 {
		displayText = "ROOT"
	} else {
		displayText = string(node.Text)
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

	sortedChildren := make([]byte, 0, len(node.Children))
	for char := range node.Children {
		sortedChildren = append(sortedChildren, char)
	}
	sort.Slice(sortedChildren, func(i, j int) bool {
		return sortedChildren[i] < sortedChildren[j]
	})

	newPrefix := prefix + "   "
	for _, char := range sortedChildren {
		child := node.Children[char]
		t.printNode(child, newPrefix, result)
	}
}

// longestPrefix returns the length of the longest common prefix between two byte slices.
// This is a helper function used for prefix matching and node splitting.
func longestPrefix(a, b []byte) int {
	i := 0
	for ; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return i
}
