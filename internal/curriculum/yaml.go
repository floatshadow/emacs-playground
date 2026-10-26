package curriculum

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// DecodeYAML decodes YAML data into v through JSON with unknown fields
// rejected (the internal/protocol types only have JSON tags). A document
// holding only comments decodes as nothing (v unchanged).
func DecodeYAML(data []byte, v any) error {
	if len(bytes.TrimSpace(stripYAMLComments(data))) == 0 {
		return nil
	}
	j, err := yaml.YAMLToJSON(data)
	if err != nil {
		return fmt.Errorf("yaml: %s", yaml.FormatError(err, false, true))
	}
	return decodeJSONStrict(j, v)
}

// DecodeYAMLFile reads and decodes a YAML file.
func DecodeYAMLFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return DecodeYAML(data, v)
}

func decodeJSONStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	return nil
}

// stripYAMLComments removes full-line comments and blank lines so that a
// file holding only comments counts as empty.
func stripYAMLComments(data []byte) []byte {
	var out [][]byte
	for _, line := range bytes.Split(data, []byte("\n")) {
		t := bytes.TrimSpace(line)
		if len(t) == 0 || t[0] == '#' {
			continue
		}
		out = append(out, line)
	}
	return bytes.Join(out, []byte("\n"))
}
