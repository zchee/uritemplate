# uritemplate

[![test][test-badge]][test]
[![pkg.go.dev][pkg.go.dev-badge]][pkg.go.dev]
[![Go module][module-badge]][module]
[![codecov.io][codecov-badge]][codecov]

`uritemplate` is a Go implementation of [URI Template RFC6570](https://tools.ietf.org/html/rfc6570) with
full functionality of URI Template Level 4.

`uritemplate` can also generate a regexp that matches expansion of the
URI Template from a URI Template.

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
