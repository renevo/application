package confighcl

import (
	"reflect"
	"time"

	"github.com/hashicorp/hcl/v2"
)

var durationType = reflect.TypeFor[time.Duration]()

var exprType = reflect.TypeFor[hcl.Expression]()
var bodyType = reflect.TypeFor[hcl.Body]()
var blockType = reflect.TypeFor[*hcl.Block]()
var attrType = reflect.TypeFor[*hcl.Attribute]()
var attrsType = reflect.TypeFor[hcl.Attributes]()
