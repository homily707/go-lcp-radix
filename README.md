# go-lcp-radix

[![codecov](https://codecov.io/gh/homily707/go-lcp-radix/branch/master/graph/badge.svg)](https://codecov.io/gh/homily707/go-lcp-radix)
[![Go Report Card](https://goreportcard.com/badge/github.com/homily707/go-lcp-radix)](https://goreportcard.com/report/github.com/homily707/go-lcp-radix)
[![Go Reference](https://pkg.go.dev/badge/github.com/homily707/go-lcp-radix?status.svg)](https://pkg.go.dev/github.com/homily707/go-lcp-radix?tab=doc)

A high-performance radix tree (prefix tree) implementation in Go with efficient longest common prefix matching.

## Features

- **Generic**: Supports any value type using Go generics
- **Longest Common Prefix Matching**: Efficient prefix-based lookup
- **Automatic Prefix Compression**: Minimizes memory usage through prefix sharing
- **Node Removal**: Safe removal of leaf nodes with automatic tree cleanup
- **Tree Visualization**: Built-in tree printing for debugging and visualization
- **Unicode Support**: Full UTF-8 support for international text
- **[TODO] Thread-Safe Operations**: All operations are non-mutating except for Insert/Remove

## Installation

```bash
go get github.com/homily707/go-lcp-radix
```

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/homily707/go-lcp-radix"
)

func main() {
    // Create a new tree
    tree := lradix.NewTree[byte, int]()
    
    // Insert key-value pairs
    tree.Insert([]byte("hello"), 1)
    tree.Insert([]byte("hey"), 2)
    tree.Insert([]byte("hi"), 3)
    
    // Print the tree structure
    fmt.Println("Tree structure:")
    fmt.Println(tree.String())
    // Tree structure:
    // └──ROOT (val: nil)
    //    └──h (val: 3)
    //       └──e (val: 2)
    //          └──llo (val: 1)
    //          └──y (val: 2)
    //       └──i (val: 3)
    
    
    // Longest common prefix matching
    lcp, result, isExact := tree.LongestCommonPrefixMatch([]byte("hello-world!"))
    fmt.Printf("LCP: %s, Match: %v, Exact: %t\n", lcp, *result, isExact) 
    // Output: LCP: hello, Match: 1, Exact: false
    lcp, result, isExact = tree.LongestCommonPrefixMatch([]byte("hey"))
    fmt.Printf("LCP: %s, Match: %v, Exact: %t\n", lcp, *result, isExact) 
    // Output: LCP: hey, Match: 2, Exact: true
    lcp, result, isExact = tree.LongestCommonPrefixMatch([]byte("hell"))
    fmt.Printf("LCP: %s, Match: %v, Exact: %t\n", lcp, *result, isExact) 
    // Output: LCP: hell, Match: 1, Exact: false
}
```
