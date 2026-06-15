package runtime

import (
	"github.com/juancavallotti/eip-go/core/internal/dsl"
	"github.com/juancavallotti/eip-go/types"
)

// LoadConfig reads and parses the runtime config from the file at path.
func LoadConfig(path string) (types.Config, error) {
	return dsl.LoadFile(path)
}

// ParseConfig parses the runtime config from raw config bytes.
func ParseConfig(data []byte) (types.Config, error) {
	return dsl.Parse(data)
}
