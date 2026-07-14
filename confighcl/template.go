package confighcl

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/gocty"
)

// AppendTemplate appends a comment-aware HCL template for val to dst. Val must
// be a non-nil pointer to a struct or map accepted by DecodeBody. Existing
// destination attributes or blocks with the same root name are rejected.
func AppendTemplate(val any, dst *hclwrite.Body) (err error) {
	if dst == nil {
		return fmt.Errorf("template destination must not be nil")
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("invalid template schema: %v", recovered)
		}
	}()

	value := reflect.ValueOf(val)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("template value must be a non-nil pointer to a struct or map")
	}
	value = value.Elem()

	var names []string
	switch value.Kind() {
	case reflect.Struct:
		tags := getFieldTags(value.Type())
		if tags.Remain != nil {
			return fmt.Errorf("cannot generate a complete template for %s with a remain field", value.Type())
		}
		names = templateNames(tags)
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("template map key type must be string, not %s", value.Type().Key())
		}
		iterator := value.MapRange()
		for iterator.Next() {
			names = append(names, iterator.Key().String())
		}
		sort.Strings(names)
	default:
		return fmt.Errorf("template value must point to a struct or map, not %s", value.Kind())
	}

	for _, name := range names {
		if !hclsyntax.ValidIdentifier(name) {
			return fmt.Errorf("template name %q is not a valid HCL identifier", name)
		}
		if bodyContainsName(dst, name) {
			return fmt.Errorf("template name %q conflicts with existing configuration", name)
		}
	}

	switch value.Kind() {
	case reflect.Struct:
		return appendStructTemplate(value, dst)
	case reflect.Map:
		return appendMapTemplate(value, names, dst)
	default:
		panic("unreachable")
	}
}

func templateNames(tags *fieldTags) []string {
	names := make([]string, 0, len(tags.Attributes)+len(tags.Blocks))
	for name := range tags.Attributes {
		names = append(names, name)
	}
	for name := range tags.Blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func bodyContainsName(body *hclwrite.Body, name string) bool {
	if body.GetAttribute(name) != nil {
		return true
	}
	for _, block := range body.Blocks() {
		if block.Type() == name {
			return true
		}
	}
	return false
}

func appendMapTemplate(value reflect.Value, names []string, dst *hclwrite.Body) error {
	for _, name := range names {
		mapValue := value.MapIndex(reflect.ValueOf(name).Convert(value.Type().Key()))
		encoded, err := templateValue(mapValue)
		if err != nil {
			return fmt.Errorf("encode map attribute %q: %w", name, err)
		}
		dst.SetAttributeValue(name, encoded)
	}
	return nil
}

func appendStructTemplate(value reflect.Value, dst *hclwrite.Body) error {
	tags := getFieldTags(value.Type())
	if tags.Remain != nil {
		return fmt.Errorf("cannot generate a complete template for %s with a remain field", value.Type())
	}

	type fieldEntry struct {
		name  string
		index int
		block bool
	}
	fields := make([]fieldEntry, 0, len(tags.Attributes)+len(tags.Blocks))
	for name, index := range tags.Attributes {
		fields = append(fields, fieldEntry{name: name, index: index})
	}
	for name, index := range tags.Blocks {
		fields = append(fields, fieldEntry{name: name, index: index, block: true})
	}
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].index < fields[j].index })

	for _, entry := range fields {
		field := value.Field(entry.index)
		fieldType := value.Type().Field(entry.index).Type
		description := tags.Descriptions[entry.index]
		if entry.block {
			if err := appendBlockFieldTemplate(entry.name, description, field, fieldType, dst); err != nil {
				return err
			}
			continue
		}

		if isUnsupportedAttributeType(fieldType) {
			return fmt.Errorf("cannot synthesize attribute %q from %s", entry.name, fieldType)
		}
		appendDescription(dst, description)
		if fieldType.Kind() == reflect.Pointer && field.IsNil() {
			zero := reflect.New(fieldType.Elem()).Elem()
			encoded, err := templateValue(zero)
			if err != nil {
				return fmt.Errorf("encode attribute %q: %w", entry.name, err)
			}
			appendCommentedAttribute(dst, entry.name, encoded)
			continue
		}
		encoded, err := templateValue(field)
		if err != nil {
			return fmt.Errorf("encode attribute %q: %w", entry.name, err)
		}
		dst.SetAttributeValue(entry.name, encoded)
	}
	return nil
}

func appendBlockFieldTemplate(name, description string, field reflect.Value, fieldType reflect.Type, dst *hclwrite.Body) error {
	if fieldType.Kind() == reflect.Array {
		return fmt.Errorf("cannot synthesize block %q from array %s; configuration block collections must be slices", name, fieldType)
	}
	sequence := fieldType.Kind() == reflect.Slice
	elementType := fieldType
	if sequence {
		elementType = fieldType.Elem()
	}
	if elementType == blockType {
		return fmt.Errorf("cannot synthesize block %q from raw %s", name, elementType)
	}
	pointer := elementType.Kind() == reflect.Pointer
	if pointer {
		elementType = elementType.Elem()
	}
	if elementType.Kind() != reflect.Struct || bodyType.AssignableTo(elementType) || attrsType.AssignableTo(elementType) {
		return fmt.Errorf("cannot synthesize block %q from %s", name, fieldType)
	}

	if sequence && field.Len() == 0 || !sequence && pointer && field.IsNil() {
		appendDescription(dst, description)
		block, err := newTemplateBlock(reflect.New(elementType).Elem(), name, true)
		if err != nil {
			return err
		}
		appendCommentedBlock(dst, block)
		return nil
	}

	if sequence {
		for index := range field.Len() {
			item := field.Index(index)
			if pointer {
				if item.IsNil() {
					return fmt.Errorf("cannot synthesize block %q from nil element at index %d", name, index)
				}
				item = item.Elem()
			}
			appendDescription(dst, description)
			block, err := newTemplateBlock(item, name, false)
			if err != nil {
				return err
			}
			dst.AppendBlock(block)
			dst.AppendNewline()
		}
		return nil
	}

	if pointer {
		field = field.Elem()
	}
	appendDescription(dst, description)
	block, err := newTemplateBlock(field, name, false)
	if err != nil {
		return err
	}
	dst.AppendBlock(block)
	dst.AppendNewline()
	return nil
}

func newTemplateBlock(value reflect.Value, name string, placeholder bool) (*hclwrite.Block, error) {
	tags := getFieldTags(value.Type())
	labels := make([]string, len(tags.Labels))
	for index, label := range tags.Labels {
		labelField := value.Type().Field(label.FieldIndex)
		if labelField.Type.Kind() != reflect.String || !value.Field(label.FieldIndex).CanInterface() {
			return nil, fmt.Errorf("block %q label %q must be an exported string field", name, label.Name)
		}
		if placeholder {
			labels[index] = "example"
		} else {
			labels[index] = fmt.Sprint(value.Field(label.FieldIndex).Interface())
		}
	}
	block := hclwrite.NewBlock(name, labels)
	if err := appendStructTemplate(value, block.Body()); err != nil {
		return nil, fmt.Errorf("encode block %q: %w", name, err)
	}
	return block, nil
}

func templateValue(value reflect.Value) (cty.Value, error) {
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return cty.NilVal, fmt.Errorf("value is nil")
		}
		value = value.Elem()
	}
	if !value.IsValid() || !value.CanInterface() {
		return cty.NilVal, fmt.Errorf("value cannot be represented")
	}
	if value.Type() == durationType {
		return cty.StringVal(time.Duration(value.Int()).String()), nil
	}
	valueType, err := gocty.ImpliedType(value.Interface())
	if err != nil {
		return cty.NilVal, err
	}
	return gocty.ToCtyValue(value.Interface(), valueType)
}

func isUnsupportedAttributeType(valueType reflect.Type) bool {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return exprType.AssignableTo(valueType) || attrType.AssignableTo(valueType)
}

func appendDescription(dst *hclwrite.Body, description string) {
	if description == "" {
		return
	}
	appendDescriptionSpacing(dst)
	var source strings.Builder
	for _, line := range strings.Split(description, "\n") {
		source.WriteString("# ")
		source.WriteString(line)
		source.WriteByte('\n')
	}
	dst.AppendUnstructuredTokens(hclwrite.Tokens{commentToken(source.String())})
}

func appendDescriptionSpacing(dst *hclwrite.Body) {
	source := dst.BuildTokens(nil).Bytes()
	if len(source) != 0 && !bytes.HasSuffix(source, []byte("\n\n")) {
		dst.AppendNewline()
	}
}

func appendCommentedAttribute(dst *hclwrite.Body, name string, value cty.Value) {
	body := hclwrite.NewEmptyFile().Body()
	body.SetAttributeValue(name, value)
	dst.AppendUnstructuredTokens(commentedTokens(body.BuildTokens(nil).Bytes()))
}

func appendCommentedBlock(dst *hclwrite.Body, block *hclwrite.Block) {
	dst.AppendUnstructuredTokens(commentedTokens(block.BuildTokens(nil).Bytes()))
	dst.AppendNewline()
}

func commentedTokens(source []byte) hclwrite.Tokens {
	formatted := hclwrite.Format(source)
	var result strings.Builder
	for _, line := range bytes.Split(bytes.TrimSuffix(formatted, []byte("\n")), []byte("\n")) {
		result.WriteString("# ")
		result.Write(line)
		result.WriteByte('\n')
	}
	return hclwrite.Tokens{commentToken(result.String())}
}

func commentToken(source string) *hclwrite.Token {
	return &hclwrite.Token{Type: hclsyntax.TokenComment, Bytes: []byte(source)}
}
