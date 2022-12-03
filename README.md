[![Tests](https://github.com/netascode/go-sdwan/actions/workflows/test.yml/badge.svg)](https://github.com/netascode/go-sdwan/actions/workflows/test.yml)

# go-sdwan

`go-sdwan` is a Go client library for Cisco SDWAN. It is based on Nathan's excellent [goaci](https://github.com/brightpuddle/goaci) module and features a simple, extensible API and [advanced JSON manipulation](#result-manipulation).

## Getting Started

### Installing

To start using `go-sdwan`, install Go and `go get`:

`$ go get -u github.com/netascode/go-sdwan`

### Basic Usage

```go
package main

import "github.com/netascode/go-sdwan"

func main() {
    client, _ := sdwan.NewClient("1.1.1.1", "user", "pwd", true)

    res, _ := client.Get("/admin/resourcegroup")
    println(res.Get("0.id").String())
}
```

This will print something like:

```
0:RESOURCE_GROUPNode:1626378545017:2
```

#### Result manipulation

`sdwan.Result` uses GJSON to simplify handling JSON results. See the [GJSON](https://github.com/tidwall/gjson) documentation for more detail.

```go
res, _ := client.Get("/admin/resourcegroup")

for _, group := range res.Array() {
    println(group.Get("@pretty").String()) // pretty print resource groups
}
```

#### POST data creation

`sdwan.Body` is a wrapper for [SJSON](https://github.com/tidwall/sjson). SJSON supports a path syntax simplifying JSON creation.

```go
body := sdwan.Body{}.
    Set("name", "test").
    Set("desc", "API Test")
client.Post("/admin/resourcegroup", body.Str)
```

## Documentation

See the [documentation](https://godoc.org/github.com/netascode/go-sdwan) for more details.
