module github.com/alsotoes/momo/src/storage

go 1.25.10

replace github.com/alsotoes/momo/src/common => ../common

require (
	github.com/alsotoes/momo/src/common v0.0.0-00010101000000-000000000000
	go.etcd.io/bbolt v1.4.3
)

require (
	golang.org/x/sys v0.29.0 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
)
