package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/json"
	"github.com/renevo/config"
	"github.com/zclconf/go-cty/cty"

	"github.com/renevo/application/confighcl"
	"github.com/renevo/application/confighcl/funcs"
)

// Configuration decodes native HCL or JSON into an application's registered
// settings and structured bindings. Decoding requires a context carrying an
// Application, normally one created for a lifecycle hook.
type Configuration struct{}

type settingSchema struct {
	children map[string]*settingSchema
	path     string
}

type hclSettingsSource struct {
	values []config.RawValue
}

func (source hclSettingsSource) Load(context.Context) ([]config.RawValue, error) {
	return slices.Clone(source.values), nil
}

// DecodeFile reads filename and performs an initial configuration load. The
// filename extension selects native HCL or JSON syntax. Read, parse, schema,
// validation, and commit failures are returned as HCL diagnostics.
func (c Configuration) DecodeFile(ctx context.Context, filename string) hcl.Diagnostics {
	return c.decodeFile(ctx, filename, false)
}

// ReloadFile reads filename and atomically reloads registered settings.
// Structured targets registered with Context.BindConfig are validated but not
// reassigned. Failures are returned as diagnostics and preserve committed settings.
func (c Configuration) ReloadFile(ctx context.Context, filename string) hcl.Diagnostics {
	return c.decodeFile(ctx, filename, true)
}

func (c Configuration) decodeFile(ctx context.Context, filename string, reload bool) hcl.Diagnostics {
	src, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return hcl.Diagnostics{
				{
					Severity: hcl.DiagError,
					Summary:  "Configuration file not found",
					Detail:   fmt.Sprintf("The configuration file %s does not exist.", filename),
				},
			}
		}

		return hcl.Diagnostics{
			{
				Severity: hcl.DiagError,
				Summary:  "Failed to read configuration",
				Detail:   fmt.Sprintf("Can't read %s: %s.", filename, err),
			},
		}
	}

	return c.decode(ctx, filename, src, reload)
}

// Decode performs an initial configuration load from src. The filename is used
// to select HCL or JSON syntax and to identify diagnostic source ranges. The
// context must carry an Application.
func (c Configuration) Decode(ctx context.Context, filename string, src []byte) hcl.Diagnostics {
	return c.decode(ctx, filename, src, false)
}

func (c Configuration) decode(ctx context.Context, filename string, src []byte, reload bool) hcl.Diagnostics {
	var file *hcl.File
	var diags hcl.Diagnostics

	switch suffix := strings.ToLower(filepath.Ext(filename)); suffix {
	case ".hcl":
		file, diags = hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	case ".json":
		file, diags = json.Parse(src, filename)
	default:
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Unsupported file format",
			Detail:   fmt.Sprintf("Cannot read from %s: unrecognized file format suffix %q.", filename, suffix),
		})
		return diags
	}
	if diags.HasErrors() {
		return diags
	}

	app := FromContext(ctx)
	if app == nil {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Application context missing",
			Detail:   "Configuration decoding requires an application context.",
		})
	}

	schema, schemaDiags := settingBodySchema(app.settings)
	diags = diags.Extend(schemaDiags)
	if diags.HasErrors() {
		return diags
	}

	values, leftovers, decodeDiags := decodeSettingBody(file.Body, schema, c.EvalContext(ctx), filename, true)
	diags = diags.Extend(decodeDiags)
	if diags.HasErrors() {
		return diags
	}

	type stagedBinding struct {
		target reflect.Value
		value  reflect.Value
	}
	staged := make([]stagedBinding, 0, len(app.configBindings))
	for _, binding := range app.configBindings {
		candidate := reflect.New(binding.target.Elem().Type())
		candidate.Elem().Set(cloneConfigValue(binding.target.Elem(), make(map[cloneVisit]reflect.Value)))
		var bindingDiags hcl.Diagnostics
		leftovers, bindingDiags = confighcl.DecodeLeftoverBody(leftovers, c.EvalContext(ctx), candidate.Interface())
		for _, diagnostic := range bindingDiags {
			if diagnostic.Detail != "" {
				diagnostic.Detail = fmt.Sprintf("Module %q: %s", binding.module, diagnostic.Detail)
			}
		}
		diags = diags.Extend(bindingDiags)
		staged = append(staged, stagedBinding{target: binding.target, value: candidate.Elem()})
	}
	if !diags.HasErrors() {
		diags = diags.Extend(confighcl.DecodeBody(leftovers, c.EvalContext(ctx), new(struct{})))
	}
	if diags.HasErrors() {
		return diags
	}

	var loadErr error
	if reload {
		loadErr = app.settings.Reload(ctx, hclSettingsSource{values: values}, app.environmentSource())
	} else {
		loadErr = app.settings.Load(ctx, hclSettingsSource{values: values}, app.environmentSource())
	}
	if loadErr != nil {
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Failed to load settings",
			Detail:   loadErr.Error(),
		})
		return diags
	}
	if !reload {
		for _, binding := range staged {
			binding.target.Elem().Set(binding.value)
		}
	}
	return diags
}

type cloneVisit struct {
	typeOf   reflect.Type
	kind     reflect.Kind
	ptr      uintptr
	length   int
	capacity int
}

func cloneConfigValue(value reflect.Value, visited map[cloneVisit]reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clone := reflect.New(value.Type()).Elem()
		clone.Set(cloneConfigValue(value.Elem(), visited))
		return clone
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		visit := cloneVisit{typeOf: value.Type(), kind: value.Kind(), ptr: value.Pointer()}
		if clone, ok := visited[visit]; ok {
			return clone
		}
		clone := reflect.New(value.Type().Elem())
		visited[visit] = clone
		clone.Elem().Set(cloneConfigValue(value.Elem(), visited))
		return clone
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		visit := cloneVisit{typeOf: value.Type(), kind: value.Kind(), ptr: value.Pointer()}
		if clone, ok := visited[visit]; ok {
			return clone
		}
		clone := reflect.MakeMapWithSize(value.Type(), value.Len())
		visited[visit] = clone
		iterator := value.MapRange()
		for iterator.Next() {
			clone.SetMapIndex(iterator.Key(), cloneConfigValue(iterator.Value(), visited))
		}
		return clone
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clone := reflect.MakeSlice(value.Type(), value.Len(), value.Cap())
		if value.Pointer() != 0 {
			visit := cloneVisit{
				typeOf:   value.Type(),
				kind:     value.Kind(),
				ptr:      value.Pointer(),
				length:   value.Len(),
				capacity: value.Cap(),
			}
			if existing, ok := visited[visit]; ok {
				return existing
			}
			visited[visit] = clone
		}
		for index := range value.Len() {
			clone.Index(index).Set(cloneConfigValue(value.Index(index), visited))
		}
		return clone
	case reflect.Struct:
		clone := reflect.New(value.Type()).Elem()
		clone.Set(value)
		for index := range value.NumField() {
			if clone.Field(index).CanSet() && value.Field(index).CanInterface() {
				clone.Field(index).Set(cloneConfigValue(value.Field(index), visited))
			}
		}
		return clone
	case reflect.Array:
		clone := reflect.New(value.Type()).Elem()
		for index := range value.Len() {
			clone.Index(index).Set(cloneConfigValue(value.Index(index), visited))
		}
		return clone
	default:
		return value
	}
}

func settingBodySchema(settings *config.Set) (*settingSchema, hcl.Diagnostics) {
	root := &settingSchema{children: make(map[string]*settingSchema)}
	var diags hcl.Diagnostics
	settings.Range(func(path string, _ *config.Setting) bool {
		segments := strings.Split(path, ".")
		current := root
		for index, segment := range segments {
			name := strings.ToLower(segment)
			if !hclsyntax.ValidIdentifier(name) {
				diags = diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid setting name",
					Detail:   fmt.Sprintf("Setting path %q contains %q, which is not a valid HCL identifier.", path, name),
				})
				return true
			}
			child := current.children[name]
			if child == nil {
				child = &settingSchema{children: make(map[string]*settingSchema)}
				current.children[name] = child
			}
			current = child
			if index == len(segments)-1 {
				if len(current.children) != 0 {
					diags = appendSettingConflict(diags, path)
				}
				current.path = path
			} else if current.path != "" {
				diags = appendSettingConflict(diags, current.path)
			}
		}
		return true
	})
	return root, diags
}

func appendSettingConflict(diags hcl.Diagnostics, path string) hcl.Diagnostics {
	return diags.Append(&hcl.Diagnostic{
		Severity: hcl.DiagError,
		Summary:  "Conflicting setting path",
		Detail:   fmt.Sprintf("Setting %q is both a value and a parent configuration block.", path),
	})
}

func decodeSettingBody(body hcl.Body, schema *settingSchema, evalContext *hcl.EvalContext, source string, partial bool) ([]config.RawValue, hcl.Body, hcl.Diagnostics) {
	bodySchema := &hcl.BodySchema{}
	for name, child := range schema.children {
		if child.path != "" {
			bodySchema.Attributes = append(bodySchema.Attributes, hcl.AttributeSchema{Name: name})
		} else {
			bodySchema.Blocks = append(bodySchema.Blocks, hcl.BlockHeaderSchema{Type: name})
		}
	}
	var content *hcl.BodyContent
	var leftovers hcl.Body
	var diags hcl.Diagnostics
	if partial {
		content, leftovers, diags = body.PartialContent(bodySchema)
	} else {
		content, diags = body.Content(bodySchema)
	}
	if content == nil {
		return nil, leftovers, diags
	}

	values := make([]config.RawValue, 0, len(content.Attributes))
	for name, attribute := range content.Attributes {
		value, valueDiags := attribute.Expr.Value(evalContext)
		diags = diags.Extend(valueDiags)
		if valueDiags.HasErrors() {
			continue
		}
		text, err := settingText(value)
		if err != nil {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Unsupported setting value",
				Detail:   fmt.Sprintf("Setting %q: %v.", schema.children[name].path, err),
				Subject:  attribute.Expr.Range().Ptr(),
			})
			continue
		}
		values = append(values, config.RawValue{Path: schema.children[name].path, Value: text, Source: source})
	}

	blocks := content.Blocks.ByType()
	for name, child := range schema.children {
		if child.path != "" {
			continue
		}
		matching := blocks[name]
		if len(matching) > 1 {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("Duplicate %s block", name),
				Detail:   fmt.Sprintf("Only one %s block is allowed.", name),
				Subject:  &matching[1].DefRange,
			})
			continue
		}
		if len(matching) == 1 {
			nested, _, nestedDiags := decodeSettingBody(matching[0].Body, child, evalContext, source, false)
			values = append(values, nested...)
			diags = diags.Extend(nestedDiags)
		}
	}
	return values, leftovers, diags
}

func settingText(value cty.Value) (string, error) {
	if !value.IsKnown() || value.IsNull() {
		return "", errors.New("value must be known and non-null")
	}
	switch value.Type() {
	case cty.String:
		return value.AsString(), nil
	case cty.Bool:
		return fmt.Sprint(value.True()), nil
	case cty.Number:
		return value.AsBigFloat().Text('f', -1), nil
	default:
		return "", fmt.Errorf("type %s is not a scalar setting", value.Type().FriendlyName())
	}
}

// EvalContext returns the expression environment used by configuration loads.
// It contains Stdlib functions and host.name. When ctx carries an Application,
// application.name and application.version are also available.
func (Configuration) EvalContext(ctx context.Context) *hcl.EvalContext {
	var result hcl.EvalContext

	// functions
	result.Functions = funcs.Stdlib()

	// variables
	allMap := map[string]any{}

	if hs, err := os.Hostname(); err == nil {
		_ = addNestedKey(allMap, "host.name", hs)
	}

	if app := FromContext(ctx); app != nil {
		_ = addNestedKey(allMap, "application.name", app.name)
		_ = addNestedKey(allMap, "application.version", app.version)
	}

	var err error
	// if we put in something bad, panic
	if result.Variables, err = ctyify(allMap); err != nil {
		panic(err)
	}

	return &result
}

// addNestedKey expands keys into their nested form:
//
//	k="foo.bar", v="quux" -> {"foo": {"bar": "quux"}}
//
// Existing keys are overwritten. Map values take precedence over primitives.
//
// If the key has dots but cannot be converted to a valid nested data structure
// (eg "foo...bar", "foo.", or non-object value exists for key), an error is
// returned.
func addNestedKey(dst map[string]any, k, v string) error {
	// createdParent and Key capture the parent object of the first created
	// object and the first created object's key respectively. The cleanup
	// func deletes them to prevent side-effects when returning errors.
	var createdParent map[string]any
	var createdKey string
	cleanup := func() {
		if createdParent != nil {
			delete(createdParent, createdKey)
		}
	}

	segments := strings.Split(k, ".")
	for _, newKey := range segments[:len(segments)-1] {
		if newKey == "" {
			// String either begins with a dot (.foo) or has at
			// least two consecutive dots (foo..bar); either way
			// it's an invalid object path.
			cleanup()
			return ErrInvalidObjectPath
		}

		var target map[string]any
		if existingI, ok := dst[newKey]; ok {
			if existing, ok := existingI.(map[string]any); ok {
				// Target already exists
				target = existing
			} else {
				// Existing value is not a map. Maps should
				// take precedence over primitive values (eg
				// overwrite attr.driver.qemu = "1" with
				// attr.driver.qemu.version = "...")
				target = make(map[string]any)
				dst[newKey] = target
			}
		} else {
			// Does not exist, create
			target = make(map[string]any)
			dst[newKey] = target

			// If this is the first created key, capture it for
			// cleanup if there is an error later.
			if createdParent == nil {
				createdParent = dst
				createdKey = newKey
			}
		}

		// Descend into new m
		dst = target
	}

	// See if the final segment is a valid key
	newKey := segments[len(segments)-1]
	if newKey == "" {
		// String ends in a dot
		cleanup()
		return ErrInvalidObjectPath
	}

	if existingI, ok := dst[newKey]; ok {
		if _, ok := existingI.(map[string]any); ok {
			// Existing value is a map which takes precedence over
			// a primitive value. Drop primitive.
			return nil
		}
	}
	dst[newKey] = v
	return nil
}

// ctyify converts nested map[string]interfaces to a map[string]cty.Value. An
// error is returned if an unsupported type is encountered.
//
// Currently only strings, cty.Values, and nested maps are supported.
func ctyify(src map[string]any) (map[string]cty.Value, error) {
	dst := make(map[string]cty.Value, len(src))

	for k, vI := range src {
		switch v := vI.(type) {
		case string:
			dst[k] = cty.StringVal(v)

		case cty.Value:
			dst[k] = v

		case map[string]any:
			o, err := ctyify(v)
			if err != nil {
				return nil, err
			}
			dst[k] = cty.ObjectVal(o)

		default:
			return nil, fmt.Errorf("key %q has invalid type %T", k, v)
		}
	}

	return dst, nil
}
