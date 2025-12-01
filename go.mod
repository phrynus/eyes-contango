module main

go 1.25.1

require (
	github.com/ccxt/ccxt/go/v4 v4.5.20
	github.com/ccxt/ccxt/go/v4/pro v0.0.0-20251121161312-ce3da4a4ec63
	github.com/gdamore/tcell/v2 v2.8.1
	github.com/rivo/tview v0.42.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/ccxt/ccxt/go/v4 => ./v4

replace github.com/ccxt/ccxt/go/v4/pro => ./v4/pro

require (
	github.com/bits-and-blooms/bitset v1.20.0 // indirect
	github.com/consensys/gnark-crypto v0.18.1 // indirect
	github.com/crate-crypto/go-eth-kzg v1.4.0 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.3 // indirect
	github.com/ethereum/go-ethereum v1.16.5 // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/supranational/blst v0.3.16-0.20250831170142-f48500c1fdbe // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/term v0.37.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)
