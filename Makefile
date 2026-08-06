.PHONY: build test validate install clean

build:
	HEC_VERSION=$${HEC_VERSION:?HEC_VERSION is required} ./scripts/build.sh

test:
	GOROOT=/opt/hec/toolchains/go/1.26.2 PATH=/opt/hec/toolchains/go/1.26.2/bin:$$PATH go test ./...
	./scripts/validate-maintenance.sh

validate:
	./scripts/validate-maintenance.sh
	./scripts/validate-forge.sh

install:
	HEC_VERSION=$${HEC_VERSION:?HEC_VERSION is required} ./scripts/install.sh

clean:
	rm -rf dist
