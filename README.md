# depstubber

![CI](https://github.com/github/depstubber/workflows/CI/badge.svg)

This is a tool that generates type-correct stubs for dependencies, for use in
testing. It is particularly useful for testing static analysis applications,
where including library source code may be undesirable. It was written and is currently
used for testing [CodeQL Go](https://github.com/github/codeql-go).

The general usage pattern if vendoring is desired will look something like:

```sh
PATH="$PATH:$GOPATH/bin"
GO111MODULES=off go get github.com/github/depstubber
GO111MODULES=off go install github.com/github/depstubber # Only needed for Go 1.18 and above
go mod tidy # required to generate go.sum
# generate a vendor/module.txt for the go 1.24 vendor consistency check
depstubber -write_module_txt
# Create stubs of Type1, Type2, SomeFunc, and SomeVariable. They are separated by a space
# because values must be treated differently from types in the implementation of depstubber.
depstubber -vendor github.com/my/package Type1,Type2 SomeFunc,SomeVariable
```

The last line can be executed using Go's built in `generate` subcommand, by
adding a comment to any relevant Go file that looks like this:

```go
//go:generate depstubber -vendor github.com/my/package Type1,Type2 SomeFunc,SomeVariable
```

Then, run `go generate <package>`, where `<package>` is the package containing
the file the comment was added to. This will automatically run the depstubber
command.

Limitations:

 - It is limited to a single package at a time.
 - There is no way to automatically stub all exports.
 - It does not generate memory-compatible types, as unexported types are
   skipped.
 - There is no way to automatically detect exports used in a program.
 - There is no way to specify specific methods on a type; all methods are
   automatically stubbed.
 - It cannot currently distinguish between type aliases. This is a
   limitation of the `reflect` package.

Please feel free to submit a [pull
request](https://github.com/github/depstubber/pulls) for any of the above, or
with any other improvements. See [CONTRIBUTING.md](CONTRIBUTING.md) for more
information.

For information about what improvements may be in progress, see
[issues](https://github.com/github/depstubber/issues).

To build, simply run `go build` with Go 1.14 or higher.

This project contains a significant amount of code copied from the [GoMock
project](https://github.com/golang/mock), as well as from the [Go standard
library](https://github.com/golang/go). The mock code has been adapted to
generate stub versions of requested exported fields instead of generating a mock
implementation of an interface. The licenses for both these codebases can be
found in the `NOTICE` file.

It is licensed under Apache-2.0. For more information, see the `LICENSE` file.
