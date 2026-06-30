module github.com/alsotoes/momo/src/transport

go 1.25.10

require (
	github.com/quic-go/quic-go v0.60.0
	go.uber.org/goleak v1.3.0
)

require (
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
)

replace github.com/alsotoes/momo/src/common => ../common
