module github.com/alyeri/nextendo-mk8d-matchmaking-v2/server

go 1.24

require (
	github.com/NextendoNetwork/nextendo-nex v0.1.2
	github.com/redis/go-redis/v9 v9.22.0
)

replace github.com/NextendoNetwork/nextendo-nex => ../nextendo-nex

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/lxzan/gws v1.10.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
)
