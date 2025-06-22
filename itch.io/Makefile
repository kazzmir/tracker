.PHONY: all.zip

all.zip:
	$(MAKE) -C .. wasm
	cp ../play.wasm .
	zip all.zip go1.24.0-wasm.js index.html play.wasm
