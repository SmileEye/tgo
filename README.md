# tgo: the function tracer for Go programs.

[![GoDoc](https://godoc.org/github.com/ks888/tgo?status.svg)](https://godoc.org/github.com/ks888/tgo/lib/tracer)
[![Build Status](https://travis-ci.com/ks888/tgo.svg?branch=master)](https://travis-ci.com/ks888/tgo)
[![Go Report Card](https://goreportcard.com/badge/github.com/ks888/tgo)](https://goreportcard.com/report/github.com/ks888/tgo)

### Example

This example traces the functions called between `tracer.Start()` and `tracer.Stop()`.

```golang
% cat fibonacci.go
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/ks888/tgo/lib/tracer"
)

func fib(n int) int {
	if n == 0 || n == 1 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	n, _ := strconv.ParseInt(os.Args[1], 10, 64)

	tracer.SetTraceLevel(3)
	tracer.Start()

	val := fib(int(n))
	fmt.Println(val)

	tracer.Stop()
}
% go build fibonacci.go
% ./fibonacci 3
\ (#01) main.fib(n = 3)
|\ (#01) main.fib(n = 2)
||\ (#01) main.fib(n = 1)
||/ (#01) main.fib() (~r1 = 1)
||\ (#01) main.fib(n = 0)
||/ (#01) main.fib() (~r1 = 0)
|/ (#01) main.fib() (~r1 = 1)
|\ (#01) main.fib(n = 1)
|/ (#01) main.fib() (~r1 = 1)
/ (#01) main.fib() (~r1 = 2)
\ (#01) fmt.Println(a = []{int(2)})
|\ (#01) fmt.Fprintln(a = -, w = -)
||\ (#01) fmt.newPrinter()
||/ (#01) fmt.newPrinter() (~r0 = &{arg: nil, value: {...}, fmt: {...}, reordered: false, goodArgNum: false, panicking: false, erroring: false, buf: {...}})
||\ (#01) fmt.(*pp).doPrintln(p = &{arg: nil, value: {...}, fmt: {...}, reordered: false, goodArgNum: false, panicking: false, erroring: false, buf: {...}}, a = []{int(2)})
||/ (#01) fmt.(*pp).doPrintln() ()
||\ (#01) os.(*File).Write(f = &{file: &{...}}, b = []{50, 10})
2
||/ (#01) os.(*File).Write() (n = 2, err = nil)
||\ (#01) fmt.(*pp).free(p = &{arg: int(2), value: {...}, fmt: {...}, reordered: false, goodArgNum: false, panicking: false, erroring: false, buf: {...}})
||/ (#01) fmt.(*pp).free() ()
|/ (#01) fmt.Fprintln() (n = 2, err = nil)
/ (#01) fmt.Println() (n = 2, err = nil)
```

### Install

*Note: supported go version is 1.10 or later.*

#### Mac OS X

Install the `tgo` binary and its library:

```
go get -u github.com/ks888/tgo/cmd/tgo
```

#### Linux

Install the `tgo` binary and its library:

```
go get -u github.com/ks888/tgo/cmd/tgo
```

tgo depends on the ptrace mechanism and needs to attach to the non-descendant process. For this, run the command below:

```
sudo sh -c 'echo 0 > /proc/sys/kernel/yama/ptrace_scope'
```

#### Windows

Not supported yet.

### Usage

Call `tracer.Start()` to start tracing and call `tracer.Stop()` (or just return from the caller of `tracer.Start()`) to stop tracing. That's it!

There are some options which change how detailed the traced logs are and the output writer of these logs. See the [godoc](https://godoc.org/github.com/ks888/tgo/lib/tracer) for more info.
