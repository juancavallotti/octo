module github.com/juancavallotti/eip-go/connectors

go 1.22

require (
	github.com/juancavallotti/eip-go/core v0.0.0
	github.com/juancavallotti/eip-go/types v0.0.0
)

require gopkg.in/yaml.v3 v3.0.1 // indirect

replace github.com/juancavallotti/eip-go/core => ../core

replace github.com/juancavallotti/eip-go/types => ../types
