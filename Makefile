.PHONY: build test clean

build:
	./scripts/build.sh

test:
	./scripts/test.sh

clean:
	rm -rf build
	rm -f prebuilt/linux-x86_64/NinjOSEdge prebuilt/linux-x86_64/NinjOSDashboard
