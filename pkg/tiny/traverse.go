package tiny

import (
	"fmt"
	"sort"
	"strings"
)

func (t *Tree) Traverse(indent string) {
	var params int
	if t.node != nil {
		params = len(t.node.params)
	}
	fmt.Printf("handlers=%d params=%d methods=%d names=%d\n",
		len(t.handlers), params, len(t.methods), len(t.names))
	a := make([]string, 0, len(t.methods))
	for s := range t.methods {
		a = append(a, s)
	}
	sort.Strings(a)
	for _, s := range a {
		n := t.methods[s]
		fmt.Printf("method=%s", s)
		n.output()
		n.traverse(indent, 1)
	}
}

func (x *Static) traverse(indent string, i int) {
	if x != nil {
		t := strings.Repeat(indent, i)
		j := i
		if len(x.prefix) > 0 {
			j++
		}
		fmt.Printf("%sprefix=%s indexes=%s", t, x.prefix, string(x.indexes))
		if x.below != nil {
			x.below.output()
		} else {
			fmt.Println()
		}
		for _, y := range x.constants {
			y.traverse(indent, j)
		}
		if x.below != nil {
			x.below.traverse(indent, j)
		}
	}
}

func (n *Node) output() {
	if len(n.handlers) > 0 {
		fmt.Printf(" handlers=%d params=%d", len(n.handlers), len(n.params))
	}
	fmt.Printf(" index=%d\n", n.index)
}

func (n *Node) traverse(indent string, i int) {
	t := strings.Repeat(indent, i)
	j := i + 1
	n.static.traverse(indent, i)
	for _, v := range n.variables {
		fmt.Print(t, v.tag.String())
		v.output()
		v.traverse(indent, j)
	}
}
