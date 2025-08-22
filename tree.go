package lradix

type Node[T any] struct {
	Text     []byte
	Val      T
	Children map[byte]*Node[T]
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

func (t *Tree[T]) Insert(str []byte, val T) {
	mark := t.Root
	index := 0
	for index < len(str) {
		cur := mark
		char := str[index]
		next, ok := cur.GetChild(char)
		if !ok {
			// no match, add new node to current children
			cur.AddChild(NewNode(str[index:], val))
			return
		}
		sharedPrefix := longestPrefix(next.Text, str[index:])
		if sharedPrefix < len(next.Text) {
			// partial match, split node
			// use this insert val as common node val, because it is most recent
			commonNode := NewNode(next.Text[:sharedPrefix], val)
			commonNode.AddChild(NewNode(next.Text[sharedPrefix:], next.Val))
			commonNode.AddChild(NewNode(str[index+sharedPrefix:], val))
			cur.AddChild(commonNode)
			return
		}
		// full match, move to next node
		index += sharedPrefix
		mark = next
	}
}

func (t *Tree[T]) LongestCommonPrefixMatch(str []byte) T {
	mark := t.Root
	index := 0
	for index < len(str) {
		cur := mark
		char := str[index]
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
		// complete match, move to next node
		index += sharedPrefix
	}
	return mark.Val
}

func longestPrefix(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return len(a)
}
