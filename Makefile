.PHONY: play play.wasm wasm itch.io

play:
	go mod tidy
	go build -o play ./tracker

wasm: play.wasm

play.wasm:
	env GOOS=js GOARCH=wasm go build -o play.wasm ./tracker

itch.io: wasm
	cp play.wasm itch.io
	butler push itch.io kazzmir/tracker:html

update:
	go get -u ./tracker
	go mod tidy

test:
	go test ./...
