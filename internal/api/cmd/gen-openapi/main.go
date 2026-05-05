// Command gen-openapi materializes the bcc OpenAPI 3.1 document for
// the current API surface and writes it to internal/api/openapi.json.
// It is invoked by the Makefile target `make api-openapi` and as a
// dependency of `make webui` and `make build`. It does not start a
// listener: the document comes straight from the in-memory huma
// registry (s *Server).OpenAPI() exposes.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/fgmacedo/buchecha/internal/api"
)

const outputPath = "internal/api/openapi.json"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-openapi:", err)
		os.Exit(1)
	}
}

func run() error {
	s := api.New(nil)
	api.RegisterErrorComponent(s)
	doc, err := json.MarshalIndent(s.OpenAPI(), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal openapi: %w", err)
	}
	doc = append(doc, '\n')
	if err := os.WriteFile(outputPath, doc, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}
