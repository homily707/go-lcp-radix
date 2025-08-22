package lradix

import (
	"fmt"
	"sort"
	"strings"
)

type Node[T any] struct {
	Text     []byte
	Val      T
	Children map[byte]*Node[T]
	Parent   *Node[T]
}

func NewNode[T any](text []byte, val T) *Node[T] {
	return &Node[T]{
		Text:     text,
		Val:      val,
		Children: map[byte]*Node[T]{},
	}
}

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

func (n *Node[T]) GetChild(char byte) (*Node[T], bool) {
	child, ok := n.Children[char]
	return child, ok
}

type Tree[T any] struct {
	Root *Node[T]
}

func NewTree[T any]() *Tree[T] {
	return &Tree[T]{
		Root: &Node[T]{
			Text:     []byte{},
			Children: map[byte]*Node[T]{},
		},
	}
}

func (t *Tree[T]) Insert(str []byte, val T) *Node[T] {
	mark := t.Root
	index := 0
	for index < len(str) {
		cur := mark
		char := str[index]
		next, ok := cur.GetChild(char)
		if !ok {
			// no match, add new node to current children
			newNode := NewNode(str[index:], val)
			cur.AddChild(newNode)
			return newNode
		}
		sharedPrefix := longestPrefix(next.Text, str[index:])
		if sharedPrefix < len(next.Text) {
			// partial match, split node
			// use this insert val as common node val, because it is most recent
			commonNode := NewNode(next.Text[:sharedPrefix], val)
			next.Text = next.Text[sharedPrefix:]
			newNode := NewNode(str[index+sharedPrefix:], val)
			commonNode.AddChild(next)
			commonNode.AddChild(newNode)
			cur.AddChild(commonNode)
			return newNode
		}
		// full match, move to next node
		index += sharedPrefix
		mark = next
	}
	return nil
}

func (t *Tree[T]) LongestCommonPrefixMatch(str []byte) T {
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
		if sharedPrefix < len(next.Text) {
			// partial match, stop
			break
		}
		// full match, move to next node
		index += sharedPrefix
	}
	return mark.Val
}

// only leaf node can be removed
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
	if len(parent.Children) == 0 {
		t.RemoveNode(parent)
	} else {
		for _, v := range parent.Children {
			parent.Val = v.Val
			break
		}
	}
}

func (t *Tree[T]) String() string {
	var result strings.Builder
	t.printNode(t.Root, "", &result)
	return result.String()
}

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
	result.WriteString(fmt.Sprintf("%v", node.Val))
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

func longestPrefix(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return len(a)
}
