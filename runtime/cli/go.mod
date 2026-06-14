module github.com/juancavallotti/eip-go/cli

go 1.23.0

require (
	github.com/juancavallotti/eip-go/connectors v0.0.0
	github.com/juancavallotti/eip-go/core v0.0.0
	github.com/juancavallotti/eip-go/processors v0.0.0
)

require (
	cel.dev/expr v0.25.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/google/cel-go v0.28.1 // indirect
	github.com/juancavallotti/eip-go/types v0.0.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20240823005443-9b4947da3948 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/juancavallotti/eip-go/connectors => ../connectors

replace github.com/juancavallotti/eip-go/core => ../core

replace github.com/juancavallotti/eip-go/processors => ../processors

replace github.com/juancavallotti/eip-go/types => ../types
