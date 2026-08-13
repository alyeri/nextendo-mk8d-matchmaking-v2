module github.com/alyeri/nextendo-mk8d-matchmaking-v2/server

go 1.23.0

require (
	github.com/NextendoNetwork/nextendo-nex v0.1.2
	github.com/redis/go-redis/v9 v9.7.3
)

replace github.com/NextendoNetwork/nextendo-nex => ../nextendo-nex

require (
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/lxzan/gws v1.10.0 // indirect
)
