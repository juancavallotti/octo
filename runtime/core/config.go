package core

import (
	"github.com/juancavallotti/eip-go/core/internal/dsl"
	"github.com/juancavallotti/eip-go/types"
)

func LoadConfig(path string) (types.Config, error) {
	return dsl.LoadFile(path)
}

func ParseConfig(data []byte) (types.Config, error) {
	return dsl.Parse(data)
}
