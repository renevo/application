# Config for HCL

This is based on [gohcl](https://github.com/hashicorp/hcl/tree/hcl2/gohcl). The primary difference is that this will instead handle partials by default to allow for more dynamic based configurations without all the top level schema defintions being required.

## Additionally, this supports both `hcl` and `config` tags with the same values.

## Template descriptions

`AppendTemplate` generates attributes and blocks from initialized Go values.
Add a separate `description` tag to place comments above generated attributes
and blocks:

```go
type route struct {
	Name   string `config:"name,label"`
	Target string `config:"target" description:"Route destination"`
}
```

Non-pointer values, including zero values, are emitted as active defaults. A
nil block pointer or empty repeated block collection produces one commented
example block. Root map bindings are emitted in key order without comments.
