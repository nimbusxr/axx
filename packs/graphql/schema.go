package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/nimbusxr/axx/core"
)

// introspection is the schema a service answers about itself.
const introspection = "introspection"

// loadSchema reads a service's schema: its SDL, a file of the project or a
// URL, or its introspection.
func loadSchema(sc *core.Scenario, s *service, client *http.Client) (*ast.Schema, error) {
	var sdl, name string
	switch {
	case s.schemaRow == introspection:
		var err error
		if sdl, err = introspect(sc.Context(), s, client); err != nil {
			return nil, fmt.Errorf("cannot introspect the %s graphql service: %w", s.name, err)
		}
		name = "the introspection of " + s.url
	case strings.HasPrefix(s.schemaRow, "http://") || strings.HasPrefix(s.schemaRow, "https://"):
		req, err := http.NewRequestWithContext(sc.Context(), http.MethodGet, s.schemaRow, nil)
		if err != nil {
			return nil, err
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("cannot read the schema %s: %w", s.schemaRow, err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("the schema %s answered %d", s.schemaRow, res.StatusCode)
		}
		sdl, name = string(raw), s.schemaRow
	default:
		p, err := sc.Suite().ResolvePath(s.schemaRow)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		sdl, name = string(raw), s.schemaRow
	}
	sources := []*ast.Source{{Name: name, Input: sdl}}
	if federated(sdl) {
		sources = append([]*ast.Source{{Name: "the federation directives", Input: federationDirectives, BuiltIn: true}}, sources...)
	}
	schema, err := gqlparser.LoadSchema(sources...)
	if err != nil {
		return nil, fmt.Errorf("%s is not a GraphQL schema: %w", name, err)
	}
	return schema, nil
}

// federated reports whether an SDL is a subgraph's, whose federation
// directives must be defined for it to load.
func federated(sdl string) bool {
	return strings.Contains(sdl, "specs.apollo.dev/federation") || strings.Contains(sdl, "@key")
}

// federationDirectives are the directives and scalars of Apollo Federation
// 1 and 2, which a subgraph's SDL uses without defining them.
const federationDirectives = `
scalar FieldSet
scalar link__Import
scalar federation__FieldSet
scalar federation__Scope
scalar federation__Policy
enum link__Purpose { SECURITY EXECUTION }
directive @link(url: String!, as: String, for: link__Purpose, import: [link__Import]) repeatable on SCHEMA
directive @key(fields: FieldSet!, resolvable: Boolean = true) repeatable on OBJECT | INTERFACE
directive @requires(fields: FieldSet!) on FIELD_DEFINITION
directive @provides(fields: FieldSet!) on FIELD_DEFINITION
directive @external(reason: String) on OBJECT | FIELD_DEFINITION
directive @shareable repeatable on OBJECT | FIELD_DEFINITION
directive @inaccessible on FIELD_DEFINITION | OBJECT | INTERFACE | UNION | ARGUMENT_DEFINITION | SCALAR | ENUM | ENUM_VALUE | INPUT_OBJECT | INPUT_FIELD_DEFINITION
directive @override(from: String!, label: String) on FIELD_DEFINITION
directive @tag(name: String!) repeatable on FIELD_DEFINITION | OBJECT | INTERFACE | UNION | ARGUMENT_DEFINITION | SCALAR | ENUM | ENUM_VALUE | INPUT_OBJECT | INPUT_FIELD_DEFINITION
directive @extends on OBJECT | INTERFACE
directive @interfaceObject on OBJECT
directive @composeDirective(name: String!) repeatable on SCHEMA
directive @authenticated on FIELD_DEFINITION | OBJECT | INTERFACE | SCALAR | ENUM
directive @requiresScopes(scopes: [[federation__Scope!]!]!) on FIELD_DEFINITION | OBJECT | INTERFACE | SCALAR | ENUM
directive @policy(policies: [[federation__Policy!]!]!) on FIELD_DEFINITION | OBJECT | INTERFACE | SCALAR | ENUM
`

const introspectionQuery = `query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types { ...FullType }
  }
}
fragment FullType on __Type {
  kind name
  fields(includeDeprecated: true) { name args { ...InputValue } type { ...TypeRef } }
  inputFields { ...InputValue }
  interfaces { ...TypeRef }
  enumValues(includeDeprecated: true) { name }
  possibleTypes { ...TypeRef }
}
fragment InputValue on __InputValue { name type { ...TypeRef } defaultValue }
fragment TypeRef on __Type {
  kind name
  ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } }
}`

type typeRef struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	OfType *typeRef `json:"ofType"`
}

func (t *typeRef) String() string {
	switch t.Kind {
	case "NON_NULL":
		return t.OfType.String() + "!"
	case "LIST":
		return "[" + t.OfType.String() + "]"
	}
	return t.Name
}

type inputValue struct {
	Name         string  `json:"name"`
	Type         typeRef `json:"type"`
	DefaultValue *string `json:"defaultValue"`
}

type fullType struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Fields []struct {
		Name string       `json:"name"`
		Args []inputValue `json:"args"`
		Type typeRef      `json:"type"`
	} `json:"fields"`
	InputFields   []inputValue            `json:"inputFields"`
	Interfaces    []typeRef               `json:"interfaces"`
	EnumValues    []struct{ Name string } `json:"enumValues"`
	PossibleTypes []typeRef               `json:"possibleTypes"`
}

// introspect asks the service for its schema, and writes it as SDL.
func introspect(ctx context.Context, s *service, client *http.Client) (string, error) {
	res, err := s.post(ctx, client, map[string]any{"query": introspectionQuery, "operationName": "IntrospectionQuery"})
	if err != nil {
		return "", err
	}
	if len(res.errors) > 0 {
		return "", fmt.Errorf("it answered errors: %s", res.errorText())
	}
	var data struct {
		Schema struct {
			QueryType        *struct{ Name string } `json:"queryType"`
			MutationType     *struct{ Name string } `json:"mutationType"`
			SubscriptionType *struct{ Name string } `json:"subscriptionType"`
			Types            []fullType             `json:"types"`
		} `json:"__schema"`
	}
	if err := json.Unmarshal(res.data, &data); err != nil || data.Schema.QueryType == nil {
		return "", fmt.Errorf("it answered what is not an introspection result")
	}
	return writeSDL(data.Schema.QueryType, data.Schema.MutationType, data.Schema.SubscriptionType, data.Schema.Types), nil
}

var builtinScalars = map[string]bool{"String": true, "Int": true, "Float": true, "Boolean": true, "ID": true}

func writeSDL(query, mutation, subscription *struct{ Name string }, types []fullType) string {
	var b strings.Builder
	b.WriteString("schema {\n  query: " + query.Name + "\n")
	if mutation != nil {
		b.WriteString("  mutation: " + mutation.Name + "\n")
	}
	if subscription != nil {
		b.WriteString("  subscription: " + subscription.Name + "\n")
	}
	b.WriteString("}\n")
	sort.Slice(types, func(i, j int) bool { return types[i].Name < types[j].Name })
	args := func(vs []inputValue) string {
		if len(vs) == 0 {
			return ""
		}
		parts := make([]string, len(vs))
		for i, v := range vs {
			parts[i] = v.Name + ": " + v.Type.String()
			if v.DefaultValue != nil {
				parts[i] += " = " + *v.DefaultValue
			}
		}
		return "(" + strings.Join(parts, ", ") + ")"
	}
	names := func(ts []typeRef) []string {
		out := make([]string, len(ts))
		for i, t := range ts {
			out[i] = t.Name
		}
		return out
	}
	for _, t := range types {
		if strings.HasPrefix(t.Name, "__") || builtinScalars[t.Name] {
			continue
		}
		switch t.Kind {
		case "SCALAR":
			b.WriteString("scalar " + t.Name + "\n")
		case "OBJECT", "INTERFACE":
			kw := "type"
			if t.Kind == "INTERFACE" {
				kw = "interface"
			}
			b.WriteString(kw + " " + t.Name)
			if len(t.Interfaces) > 0 {
				b.WriteString(" implements " + strings.Join(names(t.Interfaces), " & "))
			}
			b.WriteString(" {\n")
			for _, f := range t.Fields {
				b.WriteString("  " + f.Name + args(f.Args) + ": " + f.Type.String() + "\n")
			}
			b.WriteString("}\n")
		case "UNION":
			b.WriteString("union " + t.Name + " = " + strings.Join(names(t.PossibleTypes), " | ") + "\n")
		case "ENUM":
			b.WriteString("enum " + t.Name + " {\n")
			for _, v := range t.EnumValues {
				b.WriteString("  " + v.Name + "\n")
			}
			b.WriteString("}\n")
		case "INPUT_OBJECT":
			b.WriteString("input " + t.Name + " {\n")
			for _, f := range t.InputFields {
				b.WriteString("  " + f.Name + ": " + f.Type.String())
				if f.DefaultValue != nil {
					b.WriteString(" = " + *f.DefaultValue)
				}
				b.WriteString("\n")
			}
			b.WriteString("}\n")
		}
	}
	return b.String()
}
