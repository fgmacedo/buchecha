package api

import (
	"embed"
	"errors"
	"io/fs"

	"github.com/fgmacedo/buchecha/internal/services"
)

// SchemaFS is the embedded file system carrying the V1 JSON schemas
// authored under internal/api/schemas/. Adapters consume schema bytes
// via LoadSchema; direct access is exposed for callers that need to
// walk the tree (e.g. the schema-registry tests).
//
//go:embed schemas/*.json
var SchemaFS embed.FS

// LoadSchema returns the bytes of the named schema file under
// SchemaFS. The name is the bare filename, e.g. "error.schema.json".
// Unknown names map to services.ErrNotImplemented so protocol
// adapters render a deterministic 501 envelope without case-matching
// the missing-file error.
func LoadSchema(name string) ([]byte, error) {
	body, err := SchemaFS.ReadFile("schemas/" + name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, services.ErrNotImplemented.WithDetails(map[string]any{"schema": name})
		}
		return nil, err
	}
	return body, nil
}
