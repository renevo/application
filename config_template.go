package application

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/renevo/config"
	"github.com/zclconf/go-cty/cty"

	"github.com/renevo/application/confighcl"
)

type templateSetting struct {
	path        string
	value       string
	valueType   string
	description string
}

type templateSettingNode struct {
	setting  *templateSetting
	children map[string]*templateSettingNode
}

func (a *Application) configTemplate() ([]byte, error) {
	if _, diags := settingBodySchema(a.settings); diags.HasErrors() {
		return nil, fmt.Errorf("build scalar configuration template: %s", diags.Error())
	}

	root := &templateSettingNode{children: make(map[string]*templateSettingNode)}
	settings := make([]templateSetting, 0)
	a.settings.Range(func(path string, setting *config.Setting) bool {
		settings = append(settings, templateSetting{
			path:        path,
			value:       setting.DefaultValue,
			valueType:   setting.Type(),
			description: setting.Description,
		})
		return true
	})
	sort.Slice(settings, func(i, j int) bool { return settings[i].path < settings[j].path })
	for index := range settings {
		current := root
		for _, segment := range strings.Split(settings[index].path, ".") {
			name := strings.ToLower(segment)
			if current.children[name] == nil {
				current.children[name] = &templateSettingNode{children: make(map[string]*templateSettingNode)}
			}
			current = current.children[name]
		}
		current.setting = &settings[index]
	}

	file := hclwrite.NewEmptyFile()
	if err := appendSettingTemplate(root, file.Body()); err != nil {
		return nil, err
	}
	mapBindingSeen := false
	for _, binding := range a.configBindings {
		if mapBindingSeen {
			return nil, fmt.Errorf("module %q configuration follows a root map binding that consumes all remaining attributes", binding.module)
		}
		if binding.target.Elem().Kind() == reflect.Map {
			mapBindingSeen = true
		}
		if err := confighcl.AppendTemplate(binding.target.Interface(), file.Body()); err != nil {
			return nil, fmt.Errorf("generate configuration for module %q: %w", binding.module, err)
		}
	}
	return hclwrite.Format(file.Bytes()), nil
}

func appendSettingTemplate(node *templateSettingNode, body *hclwrite.Body) error {
	names := make([]string, 0, len(node.children))
	for name := range node.children {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		child := node.children[name]
		if child.setting != nil {
			value, err := scalarTemplateValue(*child.setting)
			if err != nil {
				return err
			}
			appendTemplateDescription(body, child.setting.description)
			body.SetAttributeValue(name, value)
			continue
		}

		block := hclwrite.NewBlock(name, nil)
		if err := appendSettingTemplate(child, block.Body()); err != nil {
			return err
		}
		body.AppendBlock(block)
		body.AppendNewline()
	}
	return nil
}

func scalarTemplateValue(setting templateSetting) (cty.Value, error) {
	switch setting.valueType {
	case "bool":
		switch setting.value {
		case "true":
			return cty.BoolVal(true), nil
		case "false":
			return cty.BoolVal(false), nil
		default:
			return cty.NilVal, fmt.Errorf("setting %q has invalid bool default %q", setting.path, setting.value)
		}
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64":
		value, err := cty.ParseNumberVal(setting.value)
		if err != nil {
			return cty.NilVal, fmt.Errorf("setting %q has invalid numeric default %q: %w", setting.path, setting.value, err)
		}
		return value, nil
	default:
		return cty.StringVal(setting.value), nil
	}
}

func appendTemplateDescription(body *hclwrite.Body, description string) {
	if description == "" {
		return
	}
	var source strings.Builder
	for _, line := range strings.Split(description, "\n") {
		source.WriteString("# ")
		source.WriteString(line)
		source.WriteByte('\n')
	}
	body.AppendUnstructuredTokens(hclwrite.Tokens{
		&hclwrite.Token{Type: hclsyntax.TokenComment, Bytes: []byte(source.String())},
	})
}
