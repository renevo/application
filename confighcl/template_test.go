package confighcl

import (
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/matryer/is"
)

func TestAppendTemplate(t *testing.T) {
	type route struct {
		Name    string        `config:"name,label"`
		Target  string        `config:"target" description:"Route destination"`
		Timeout time.Duration `config:"timeout,optional" description:"Request timeout"`
	}
	type target struct {
		Enabled bool    `config:"enabled,optional" description:"Enable routing"`
		Routes  []route `config:"route,block" description:"HTTP route"`
	}

	file := hclwrite.NewEmptyFile()
	err := AppendTemplate(&target{}, file.Body())
	is := is.New(t)
	is.NoErr(err)

	source := string(hclwrite.Format(file.Bytes()))
	is.True(strings.Contains(source, "# Enable routing\nenabled = false"))
	is.True(strings.Contains(source, "# HTTP route\n# route \"example\" {"))
	is.True(strings.Contains(source, "#   # Route destination\n#   target = \"\""))
	is.True(strings.Contains(source, "#   # Request timeout\n#   timeout = \"0s\""))
}

func TestAppendTemplateRoundTrip(t *testing.T) {
	type route struct {
		Name   string `config:"name,label"`
		Target string `config:"target"`
	}
	type target struct {
		Route []route `config:"route,block"`
	}

	want := target{Route: []route{{Name: "health", Target: "/healthz"}}}
	file := hclwrite.NewEmptyFile()
	is := is.New(t)
	is.NoErr(AppendTemplate(&want, file.Body()))
	source := hclwrite.Format(file.Bytes())

	parsed, diags := hclsyntax.ParseConfig(source, "generated.hcl", hcl.Pos{Line: 1, Column: 1})
	is.True(!diags.HasErrors())
	var got target
	diags = DecodeBody(parsed.Body, nil, &got)
	is.True(!diags.HasErrors())
	is.Equal(got, want)

	mapFile := hclwrite.NewEmptyFile()
	wantMap := map[string]int{"zeta": 2, "alpha": 1}
	is.NoErr(AppendTemplate(&wantMap, mapFile.Body()))
	parsed, diags = hclsyntax.ParseConfig(mapFile.Bytes(), "map.hcl", hcl.Pos{Line: 1, Column: 1})
	is.True(!diags.HasErrors())
	var gotMap map[string]int
	diags = DecodeBody(parsed.Body, nil, &gotMap)
	is.True(!diags.HasErrors())
	is.Equal(gotMap, wantMap)
}

func TestAppendTemplateMapAndCollision(t *testing.T) {
	is := is.New(t)
	file := hclwrite.NewEmptyFile()
	is.NoErr(AppendTemplate(&map[string]int{"zeta": 2, "alpha": 1}, file.Body()))
	source := string(hclwrite.Format(file.Bytes()))
	alphaIndex := strings.Index(source, "alpha")
	zetaIndex := strings.Index(source, "zeta")
	is.True(alphaIndex >= 0)
	is.True(alphaIndex < zetaIndex)

	err := AppendTemplate(&map[string]string{"alpha": "duplicate"}, file.Body())
	is.True(err != nil)
}

func TestAppendTemplateRejectsLossyBlocks(t *testing.T) {
	type block struct {
		Name string `config:"name,label"`
	}

	tests := []struct {
		name   string
		target any
	}{
		{
			name: "nil repeated pointer element",
			target: &struct {
				Blocks []*block `config:"block,block"`
			}{Blocks: []*block{nil}},
		},
		{
			name: "raw HCL block",
			target: &struct {
				Block *hcl.Block `config:"block,block"`
			}{},
		},
		{
			name: "non-string label",
			target: &struct {
				Blocks []struct {
					Name int `config:"name,label"`
				} `config:"block,block"`
			}{},
		},
		{
			name: "array block collection",
			target: &struct {
				Blocks [1]block `config:"block,block"`
			}{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := AppendTemplate(test.target, hclwrite.NewEmptyFile().Body())
			is.New(t).True(err != nil)
		})
	}
}
