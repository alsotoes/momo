module github.com/alsotoes/momo/src/server

go 1.24.0

require github.com/alsotoes/momo/src/common v0.0.0-20260604213252-d8e9e90c2b38

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/alsotoes/momo/src/common => ../common

replace github.com/alsotoes/momo/src/transport => ../transport

replace github.com/alsotoes/momo/src/client => ../client
