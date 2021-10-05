package parser

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"io/ioutil"
	"strings"
	"text/template"
	"go/ast"
	"regexp"
	"golang.org/x/tools/imports"
)

var re = regexp.MustCompile(`json:"(\d|\w+)(,omitempty)?"`)

type Entity struct {
	Name 			string
	Description		string
	Response		*Response
	Parameters 		[]Parameter
	ParameterByName map[string]Parameter
}

type Parameter struct {
	Field       string
	Type        string
	Description string
	Tag         string
}

type ResponseField struct {
	Field       string
	Type        string
	Description string
	Tag         string
}

type Response struct {
	Type   string
	Fields []ResponseField
}

func NewEntity(name string) *Entity {
	return &Entity{
		Name:        name,
		Description: "",
		Response:    nil,
	}
}

type Parser struct {
	Entities		 		[]*Entity
	EntitiesByName map[string]*Entity
	file 					*ast.File
	loggingServiceTemplate	*template.Template
}

func NewParser(name string) (*Parser, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, name, nil, 0)
	if err != nil {
		return nil, err
	}

	loggingServiceTemplate, err := ioutil.ReadFile("../templates/logging-service.go.tmpl")
	if err != nil {
		return nil, err
	}

	return &Parser{
		Entities:               make([]*Entity, 0),
		file:                   f,
		loggingServiceTemplate: template.Must(template.New("logging-service").Parse(string(loggingServiceTemplate))),
	}, nil
}

func(p *Parser) Parse() error {
	ast.Inspect(p.file, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.TypeSpec:
			if err := p.parseType(x); err != nil {
				return false
			}
		case *ast.FuncDecl:
			p.parseFunction(x)
		}
		return true
	})
	return p.generate()
}

func (p *Parser) generate() error {
	return p.generateCode(p.loggingServiceTemplate, "logging-service.go")
}

func (p *Parser) generateCode(tmpl *template.Template, fn string) error {
	fmt.Printf("Generating %s\n", fn)
	buf := bytes.NewBuffer([]byte{})
	err := tmpl.Execute(buf, p)
	if err != nil {
		return err
	}
	res, err := imports.Process(fn, buf.Bytes(), nil)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(fn, res, 0666)
}

func (p *Parser) parseType(st *ast.TypeSpec) error {
	if strings.HasSuffix(st.Name.Name, "Request") {
		endpoint := strings.Replace(st.Name.Name, "Request", "", -1)
		p.addParameter(endpoint, st.Type.(*ast.StructType))
	}
	if strings.HasSuffix(st.Name.Name, "Response") {
		endpoint := strings.Replace(st.Name.Name, "Response", "", -1)
		p.addResponseField(endpoint, st.Type)
	}
	return nil
}

func (p *Parser) addParameter(entity string, st *ast.StructType) {
	for _, field := range st.Fields.List {
		params := Parameter{
			Field:       field.Names[0].Name,
			Description: field.Doc.Text(),
			Tag:         parseTag(field.Tag.Value),
			Type:        mapFieldType(field.Type),
		}

		if p.EntitiesByName[entity] == nil {
			p.EntitiesByName[entity] = NewEntity(entity)
		}
		p.EntitiesByName[entity].Parameters = append(p.EntitiesByName[entity].Parameters, params)
		p.EntitiesByName[entity].ParameterByName[params.Field] = params
	}
}

func (p *Parser) addResponseField(entity string, expr ast.Expr) {
	if p.EntitiesByName[entity] == nil {
		p.EntitiesByName[entity] = NewEntity(entity)
	}
	response := &Response{}
	switch x := expr.(type) {
	case *ast.StructType:
		response.Type = "Object"
		response.Fields = make([]ResponseField, 0)
		for _, field := range x.Fields.List {
			responseField := ResponseField{
				Description: field.Doc.Text(),
				Field:       parseTag(field.Tag.Value),
				Type:        mapFieldType(field.Type),
			}
			response.Fields = append(response.Fields, responseField)
		}
	default:
		response.Type = mapFieldType(x)
	}
	if response.Type != "Object" || len(response.Fields) != 0 {
		p.EntitiesByName[entity].Response = response
	}
}

func mapFieldType(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		ident, ok := x.X.(*ast.Ident)
		if ok {
			return ident.Name
		}
		return "Object"
	case *ast.SelectorExpr:
		name := fmt.Sprintf("%v.%s", x.X, x.Sel.Name)
		switch name {
		case "globalid.ID":
			return "UUID"
		case "model.ReactionType":
			return "string"
		case "model.CardsResponse", "model.CardResponse", "model.Draft":
			return "Object"
		}
		return name
	case *ast.ArrayType:
		return "Array"
	default:
		panic(fmt.Sprintf("Unmapped type %T %v", x, x))
	}
}

func parseTag(tag string) string {
	match := re.FindStringSubmatch(tag)
	return match[1]
}

func (p *Parser) parseFunction(fd *ast.FuncDecl) {
	if fd.Recv == nil {
		return
	}
	if recv, ok := fd.Recv.List[0].Type.(*ast.StarExpr); ok {
		if ident, ok := recv.X.(*ast.Ident); ok {
			name := fd.Name.Name
			description := fd.Doc.Text()
			firstChar := string(name[0])
			if ident.Name == "api" && firstChar == strings.ToUpper(firstChar) {
				p.AddEndpoint(name, description)
			}
		}
	}
}

func (p *Parser) AddEndpoint(name, description string) {
	entity := p.EntitiesByName[name]
	if entity == nil {
		entity = NewEntity(name)
	}
	entity.Description = enhanceDescription(description, name)

	p.EntitiesByName[name] = entity
	p.Entities = append(p.Entities, entity)
}

func enhanceDescription(description, rpc string) string {
	return toUpper(strings.Replace(description, fmt.Sprintf("%s ", rpc), "", -1))
}

func toUpper(s string) string {
	if s == "" {
		return ""
	}
	return fmt.Sprintf("%s%s", strings.ToUpper(string(s[0])), s[1:])
}