package lradix

import (
	"fmt"
	"strings"
	"sync"
)

type ConcurrentNode[K comparable, T any] struct {
	sync.RWMutex
	Text     []K                         // Text fragment for this node (of comparable type K)
	Val      *T                          // Value associated with this node (nil for intermediate nodes, of type T)
	End      bool                        // Whether this node represents the end of a complete key
	Children map[K]*ConcurrentNode[K, T] // Child nodes indexed by first character (key type K)
	Parent   *ConcurrentNode[K, T]       // Parent node for tree traversal
}

func (n *ConcurrentNode[K, T]) GetChild(head K) (*ConcurrentNode[K, T], bool) {
	child, ok := n.Children[head]
	return child, ok
}

// must hold lock of both parent and child
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

func NewConcurrentNode[K comparable, T any](text []K, val *T, end bool) *ConcurrentNode[K, T] {
	return &ConcurrentNode[K, T]{
		Text:     text,
		Val:      val,
		End:      end,
		Children: map[K]*ConcurrentNode[K, T]{},
	}
}

type ConcurrentTree[K comparable, T any] struct {
	Root *ConcurrentNode[K, T]
}

func NewConcurrentTree[K comparable, T any]() *ConcurrentTree[K, T] {
	return &ConcurrentTree[K, T]{
		Root: NewConcurrentNode[K, T]([]K{}, nil, false),
	}
}

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
			next.Text = next.Text[sharedPrefix:]
			commonNode.AddChild(next)
			if index+sharedPrefix < len(str) {
				newNode := NewConcurrentNode(str[index+sharedPrefix:], &val, true)
				commonNode.AddChild(newNode)
				cur.Unlock()  // ===🟠===
				next.Unlock() // ===🔵===
				return newNode
			} else {
				commonNode.End = true
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

func (t *ConcurrentTree[K, T]) LongestCommonPrefixMatch(str []K) ([]K, *T, bool) {
	commonPrefix := []K{}
	mark := t.Root
	index := 0
	for index < len(str) {
		cur := mark
		char := str[index]
		// no match，stop at current node
		cur.RLock()
		next, ok := cur.GetChild(char)
		cur.RUnlock()
		if !ok {
			return commonPrefix, mark.Val, false
		}
		mark = next
		next.RLock()
		matchText := next.Text
		next.RUnlock()
		sharedPrefix := longestPrefix(matchText, str[index:])
		commonPrefix = append(commonPrefix, matchText[:sharedPrefix]...)
		if sharedPrefix < len(matchText) {
			// partial match, stop
			return commonPrefix, mark.Val, false
		}
		// full match, move to next node
		index += sharedPrefix
	}
	mark.RLock()
	defer mark.RUnlock()
	return commonPrefix, mark.Val, mark.End
}

func (t *ConcurrentTree[K, T]) RemoveNode(node *ConcurrentNode[K, T]) {
	node.Lock()
	defer node.Unlock()
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

	parent.Lock()
	delete(parent.Children, node.Text[0])
	if len(parent.Children) == 0 && !parent.End {
		parent.Unlock() // must unlock before recursive call Remove
		t.RemoveNode(parent)
	} else {
		if parent.Parent != nil {
			for _, v := range parent.Children {
				parent.Val = v.Val
				break
			}
		}
		parent.Unlock()
	}
}

func (t *ConcurrentTree[K, T]) String() string {
	var result strings.Builder
	printConcurrentNode(t.Root, "", &result)
	return result.String()
}

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
