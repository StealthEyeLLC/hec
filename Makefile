.PHONY: build test install clean

build:
	./scripts/build.sh

test:
	GOROOT=/opt/hec/toolchains/go/1.26.2 PATH=/opt/hec/toolchains/go/1.26.2/bin:$$PATH go test ./...

install:
	./scripts/install.sh

clean:
	rm -rf dist
