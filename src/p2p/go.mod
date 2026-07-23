module github.com/alsotoes/momo/src/p2p

go 1.25.10

require (
	github.com/alsotoes/momo/src/common v0.0.0-20260604213252-d8e9e90c2b38
	go.uber.org/goleak v1.3.0
)

replace github.com/alsotoes/momo/src/common => ../common
