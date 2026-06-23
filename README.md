# uritemplate

[![test][test-badge]][test]
[![pkg.go.dev][pkg.go.dev-badge]][pkg.go.dev]
[![Go module][module-badge]][module]
[![codecov.io][codecov-badge]][codecov]

`uritemplate` is a Go implementation of [URI Template RFC6570](https://tools.ietf.org/html/rfc6570) with
full functionality of URI Template Level 4.

`uritemplate` can also generate a regexp that matches expansion of the
URI Template from a URI Template.

## Performance

This `v4` line replaces the original Pike-VM matcher with a bespoke
backtracking scanner and a byte-oriented expansion path. The result is a large
speedup over `v3`, most dramatically for `Match`, where `v3` allocated a fresh
thread map on every input position.

The tables below are an apples-to-apples A/B run: the **same** templates,
values, and inputs are benchmarked against `v3.0.2` and `v4` on the same
machine with the same toolchain. Numbers are the median of `-count=6`; every
delta shown is statistically significant (`p=0.002`). Lower is better; the
`speedup` column is `v3 ns/op ÷ v4 ns/op`.

### `Match` (parse a URI back into variables)

| case (template → input)                                  | v3 ns/op | v4 ns/op | v3 allocs | v4 allocs | speedup    |
| -------------------------------------------------------- | -------: | -------: | --------: | --------: | ---------- |
| `https://{host}/users{/user}{/media}` → 3-var route      |   23,240 |      206 |       716 |         3 | **~113x**  |
| `{+path}` → reserved `/foo/bar/baz`                      |    7,204 |      177 |       221 |         5 | **~41x**   |
| `{/count*}` → exploded list                              |    8,228 |      236 |       245 |         6 | **~35x**   |
| `https://example.com/foo{?bar}` → query KV               |    4,424 |      165 |       193 |         5 | **~27x**   |
| `https://{host}/q{/term}` → percent-encoded capture      |   18,885 |      212 |       591 |         4 | **~89x**   |
| `https://example.com/foo{?bar}` → no-match               |    1,697 |       93 |       106 |         4 | **~18x**   |

### `Expand` (render a URI from variables)

| case               | v3 ns/op | v4 ns/op | v3 allocs | v4 allocs | speedup    |
| ------------------ | -------: | -------: | --------: | --------: | ---------- |
| `{var}` string     |       70 |       38 |         2 |         1 | ~1.9x      |
| `{hello}` encoded  |      130 |       53 |         3 |         1 | ~2.4x      |
| `{list}` list      |      157 |       70 |         3 |         1 | ~2.2x      |
| `{keys}` KV        |      198 |      109 |         4 |         2 | ~1.8x      |
| `{+path}` reserved |      104 |       42 |         2 |         1 | ~2.5x      |
| `{longsafe}` ~1KB  |    7,372 |      725 |        10 |         1 | **~10x**   |
| `{+longesc}` ~1KB  |   10,840 |    2,249 |        11 |         2 | ~4.8x      |

### `Compile` (`New`)

| case                                  | v3 ns/op | v4 ns/op | speedup |
| ------------------------------------- | -------: | -------: | ------- |
| `{var}`                               |       99 |       98 | ~1.0x   |
| `https://{host}/users{/user}{/media}` |      457 |      307 | ~1.5x   |
| `{+path}/{var}{?list*,keys*}{#hello}` |      465 |      356 | ~1.3x   |

`Compile/{var}` is unchanged (`p=0.937`): the smallest template was already at
its allocation floor in `v3`, so there is nothing to win there. The gains in
this row come from the larger templates that dominate real workloads.

> Benchmarked on Apple M3 Max, macOS 27.0, `go1.26.4 darwin/arm64`. Reproduce
> with `go test -run=^$ -bench=. -benchmem -count=6 ./...` against each module
> and compare with [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat).
> The benchmark matrix lives in [`bench_test.go`](./bench_test.go).

## Getting Started

## Installation

```shell
go get github.com/zchee/uritemplate/v4@latest
```

## Documentation

The documentation is available on [pkg.go.dev][pkg.go.dev].

## Examples

See [examples on GoDoc][examples].

## License

uritemplate is distributed under the BSD 3-Clause license.
PLEASE READ [LICENSE](./LICENSE) carefully and follow its clauses to use this software.

## Author

- [@yosida95](https://github.com/yosida95)
- [@zchee](https://github.com/zchee)

<!-- badge links -->
[test]: https://github.com/github.com/zchee/uritemplate/actions/workflows/test.yaml
[pkg.go.dev]: https://pkg.go.dev/github.com/zchee/uritemplate/v4
[module]: https://github.com/github.com/zchee/uritemplate/releases/latest
[codecov]: https://app.codecov.io/gh/zchee/uritemplate
[examples]: https://pkg.go.dev/github.com/zchee/uritemplate/v4#pkg-examples

[test-badge]: https://img.shields.io/github/actions/workflow/status/zchee/uritemplate/test.yaml?branch=v4&style=for-the-badge&label=TEST&logo=github
[pkg.go.dev-badge]: https://img.shields.io/badge/pkg.go.dev-doc-00add8?style=for-the-badge&logo=go
[module-badge]: https://img.shields.io/github/release/zchee/uritemplate.svg?color=00add8&label=MODULE&style=for-the-badge&logo=go
[codecov-badge]: https://img.shields.io/codecov/c/github/zchee/uritemplate/v4?logo=codecov&style=for-the-badge
