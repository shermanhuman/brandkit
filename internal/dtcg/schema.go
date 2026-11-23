package dtcg

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema/2025.10/format.json schema/2025.10/resolver.json
var schemaFS embed.FS

const (
	formatSchemaID   = "https://www.designtokens.org/schemas/2025.10/format.json"
	resolverSchemaID = "https://www.designtokens.org/schemas/2025.10/resolver.json"
)

var (
	schemaOnce     sync.Once
	formatSchema   *jsonschema.Schema
	resolverSchema *jsonschema.Schema
	schemaErr      error
)

func compileSchema(file, id string) (*jsonschema.Schema, error) {
	data, err := schemaFS.ReadFile("schema/2025.10/" + file)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	// The bundled schemas carry every sub-schema inline under its own $id,
	// so nothing is fetched from the network.
	c.UseLoader(noLoader{})
	if err := c.AddResource(id, doc); err != nil {
		return nil, err
	}
	return c.Compile(id)
}

type noLoader struct{}

func (noLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("schema %s is not bundled", url)
}

func loadSchemas() {
	formatSchema, schemaErr = compileSchema("format.json", formatSchemaID)
	if schemaErr != nil {
		return
	}
	resolverSchema, schemaErr = compileSchema("resolver.json", resolverSchemaID)
}

// ValidateFormatSchema checks a token document against the official DTCG
// 2025.10 format schema.
func ValidateFormatSchema(data []byte) error {
	return validateSchema(data, func() *jsonschema.Schema { return formatSchema })
}

// ValidateResolverSchema checks a resolver document against the official
// DTCG 2025.10 resolver schema.
func ValidateResolverSchema(data []byte) error {
	return validateSchema(data, func() *jsonschema.Schema { return resolverSchema })
}

func validateSchema(data []byte, pick func() *jsonschema.Schema) error {
	schemaOnce.Do(loadSchemas)
	if schemaErr != nil {
		return fmt.Errorf("dtcg: loading the bundled schema: %w", schemaErr)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("dtcg: %w", err)
	}
	if err := pick().Validate(inst); err != nil {
		return &SchemaError{Detail: err.Error()}
	}
	return nil
}

// SchemaError is a failure against the official JSON Schema.
type SchemaError struct{ Detail string }

func (e *SchemaError) Error() string {
	d := e.Detail
	if i := strings.Index(d, "\n"); i > 0 && len(d) > 600 {
		d = d[:600] + "…"
	}
	return "dtcg: schema: " + d
}
