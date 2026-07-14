package confighcl

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty/gocty"
)

// EncodeIntoBody replaces dst with attributes and blocks derived from val,
// which must be a struct or non-nil pointer to a struct using this package's
// field tags. Existing destination content is discarded.
//
// This function can work only with fully-decoded data. It will ignore any
// fields tagged as "remain", any fields that decode attributes into either
// hcl.Attribute or hcl.Expression values, and any fields that decode blocks
// into hcl.Attributes values. This function does not have enough information
// to complete the decoding of these types.
//
// Any fields tagged as "label" are ignored by this function. Use EncodeAsBlock
// to produce a whole hclwrite.Block including block labels.
//
// EncodeIntoBody panics for caller or schema errors, including an inappropriate
// value, a nil destination, or a field that cannot be represented as a cty
// value.
//
// The layout of the resulting HCL source is derived from the ordering of
// the struct fields, with blank lines around nested blocks of different types.
// Fields representing attributes should usually precede those representing
// blocks so that the attributes can group together in the result. For more
// control, use the hclwrite API directly.
func EncodeIntoBody(val any, dst *hclwrite.Body) {
	rv := reflect.ValueOf(val)
	ty := rv.Type()
	if ty.Kind() == reflect.Pointer {
		rv = rv.Elem()
		ty = rv.Type()
	}
	if ty.Kind() != reflect.Struct {
		panic(fmt.Sprintf("value is %s, not struct", ty.Kind()))
	}

	tags := getFieldTags(ty)
	populateBody(rv, ty, tags, dst)
}

// EncodeAsBlock creates a new blockType block populated from val, which must be
// a struct or non-nil pointer to a struct using this package's field tags.
//
// If the given struct type has fields tagged with "label" tags then they
// will be used in order to annotate the created block with labels.
//
// EncodeAsBlock has the same field support and panic contract as
// EncodeIntoBody.
func EncodeAsBlock(val any, blockType string) *hclwrite.Block {
	rv := reflect.ValueOf(val)
	ty := rv.Type()
	if ty.Kind() == reflect.Pointer {
		rv = rv.Elem()
		ty = rv.Type()
	}
	if ty.Kind() != reflect.Struct {
		panic(fmt.Sprintf("value is %s, not struct", ty.Kind()))
	}

	tags := getFieldTags(ty)
	labels := make([]string, len(tags.Labels))
	for i, lf := range tags.Labels {
		lv := rv.Field(lf.FieldIndex)
		// We just stringify whatever we find. It should always be a string
		// but if not then we'll still do something reasonable.
		labels[i] = fmt.Sprintf("%s", lv.Interface())
	}

	block := hclwrite.NewBlock(blockType, labels)
	populateBody(rv, ty, tags, block.Body())
	return block
}

func populateBody(rv reflect.Value, ty reflect.Type, tags *fieldTags, dst *hclwrite.Body) {
	nameIdxs := make(map[string]int, len(tags.Attributes)+len(tags.Blocks))
	namesOrder := make([]string, 0, len(tags.Attributes)+len(tags.Blocks))
	for n, i := range tags.Attributes {
		nameIdxs[n] = i
		namesOrder = append(namesOrder, n)
	}
	for n, i := range tags.Blocks {
		nameIdxs[n] = i
		namesOrder = append(namesOrder, n)
	}
	sort.SliceStable(namesOrder, func(i, j int) bool {
		ni, nj := namesOrder[i], namesOrder[j]
		return nameIdxs[ni] < nameIdxs[nj]
	})

	dst.Clear()

	prevWasBlock := false
	for _, name := range namesOrder {
		fieldIdx := nameIdxs[name]
		field := ty.Field(fieldIdx)
		fieldTy := field.Type
		fieldVal := rv.Field(fieldIdx)

		if fieldTy.Kind() == reflect.Pointer {
			fieldTy = fieldTy.Elem()
			fieldVal = fieldVal.Elem()
		}

		if _, isAttr := tags.Attributes[name]; isAttr {

			if exprType.AssignableTo(fieldTy) || attrType.AssignableTo(fieldTy) {
				continue // ignore undecoded fields
			}
			if !fieldVal.IsValid() {
				continue // ignore (field value is nil pointer)
			}
			if fieldTy.Kind() == reflect.Pointer && fieldVal.IsNil() {
				continue // ignore
			}
			if prevWasBlock {
				dst.AppendNewline()
				prevWasBlock = false
			}

			valTy, err := gocty.ImpliedType(fieldVal.Interface())
			if err != nil {
				panic(fmt.Sprintf("cannot encode %T as HCL expression: %s", fieldVal.Interface(), err))
			}

			val, err := gocty.ToCtyValue(fieldVal.Interface(), valTy)
			if err != nil {
				// This should never happen, since we should always be able
				// to decode into the implied type.
				panic(fmt.Sprintf("failed to encode %T as %#v: %s", fieldVal.Interface(), valTy, err))
			}

			dst.SetAttributeValue(name, val)

		} else { // must be a block, then
			elemTy := fieldTy
			isSeq := false
			if elemTy.Kind() == reflect.Slice || elemTy.Kind() == reflect.Array {
				isSeq = true
				elemTy = elemTy.Elem()
			}

			if bodyType.AssignableTo(elemTy) || attrsType.AssignableTo(elemTy) {
				continue // ignore undecoded fields
			}
			prevWasBlock = false

			if isSeq {
				l := fieldVal.Len()
				for i := range l {
					elemVal := fieldVal.Index(i)
					if !elemVal.IsValid() {
						continue // ignore (elem value is nil pointer)
					}
					if elemTy.Kind() == reflect.Pointer && elemVal.IsNil() {
						continue // ignore
					}
					block := EncodeAsBlock(elemVal.Interface(), name)
					if !prevWasBlock {
						dst.AppendNewline()
						prevWasBlock = true
					}
					dst.AppendBlock(block)
				}
			} else {
				if !fieldVal.IsValid() {
					continue // ignore (field value is nil pointer)
				}
				if elemTy.Kind() == reflect.Pointer && fieldVal.IsNil() {
					continue // ignore
				}
				block := EncodeAsBlock(fieldVal.Interface(), name)
				if !prevWasBlock {
					dst.AppendNewline()
					prevWasBlock = true
				}
				dst.AppendBlock(block)
			}
		}
	}
}
