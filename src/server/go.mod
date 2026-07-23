module github.com/alsotoes/momo/src/server

go 1.25.10

require (
	github.com/alsotoes/momo/src/client v0.0.0-00010101000000-000000000000
	github.com/alsotoes/momo/src/common v0.0.0-20260604213252-d8e9e90c2b38
	github.com/alsotoes/momo/src/storage v0.0.0-20260708003031-b3e2d20e8156
	github.com/alsotoes/momo/src/transport v0.0.0-00010101000000-000000000000
	go.uber.org/goleak v1.3.0
)

require (
	github.com/quic-go/quic-go v0.60.0 // indirect
	go.etcd.io/bbolt v1.5.0 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	gopkg.in/ini.v1 v1.67.3 // indirect
)

replace github.com/alsotoes/momo/src/common => ../common

replace github.com/alsotoes/momo/src/transport => ../transport

replace github.com/alsotoes/momo/src/client => ../client

replace github.com/alsotoes/momo/src/storage => ../storage
