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
    tree := lradix.NewTree[int]()
    
    // Insert key-value pairs
    tree.Insert([]byte("hello"), 1)
    tree.Insert([]byte("world"), 2)
    tree.Insert([]byte("hello-world"), 3)
    
    // Longest common prefix matching
    commonPrefix, result := tree.LongestCommonPrefixMatch([]byte("hello-world!"))
    fmt.Printf("Common Prefix: %s, Match: %d\n", commonPrefix, *result) // Output: Common Prefix: hello-world, Match: 3
    
    commonPrefix, result = tree.LongestCommonPrefixMatch([]byte("hello there"))
    fmt.Printf("Common Prefix: %s, Match: %d\n", commonPrefix, *result) // Output: Common Prefix: hello, Match: 1
    
    commonPrefix, result = tree.LongestCommonPrefixMatch([]byte("wo"))
    fmt.Printf("Common Prefix: %s, Match: %d\n", commonPrefix, *result) // Output: Common Prefix: wo, Match: 2
}
```
