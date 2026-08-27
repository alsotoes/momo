module github.com/alsotoes/momo/src/momofs

go 1.25.10

require (
	github.com/alsotoes/momo/src/common v0.0.0
	github.com/alsotoes/momo/src/storage v0.0.0
	go.uber.org/goleak v1.3.0
)

require (
	filippo.io/edwards25519 v1.1.1 // indirect
	filippo.io/nistec v0.0.2 // indirect
	github.com/alsotoes/momo/src/crypto v0.0.0-00010101000000-000000000000 // indirect
	github.com/bytemare/crypto v0.4.4 // indirect
	github.com/bytemare/hash v0.1.5 // indirect
	github.com/bytemare/hash2curve v0.1.3 // indirect
	github.com/gtank/ristretto255 v0.1.2 // indirect
	go.etcd.io/bbolt v1.5.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	gopkg.in/ini.v1 v1.67.3 // indirect
)

replace (
	github.com/alsotoes/momo/src/common => ../common
	github.com/alsotoes/momo/src/crypto => ../crypto
	github.com/alsotoes/momo/src/storage => ../storage
)
