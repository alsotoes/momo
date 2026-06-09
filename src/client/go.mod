module github.com/alsotoes/momo/src/client

go 1.25.0

replace github.com/alsotoes/momo/src/common => ../common

replace github.com/alsotoes/momo/src/transport => ../transport

require (
	github.com/alsotoes/momo/src/common v0.0.0-00010101000000-000000000000
	github.com/alsotoes/momo/src/transport v0.0.0-00010101000000-000000000000
)

require (
	github.com/quic-go/quic-go v0.60.0 // indirect
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
)
