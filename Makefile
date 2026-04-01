.PHONY: build-network test clean stop status lint

build-network:
	cd network && go build -o /tmp/interstellar-network github.com/vechain/interstellar-e2e/network && cd ..

test: build-network
	@/tmp/interstellar-network start & \
	NODE_URL=$$(/tmp/interstellar-network node-url) && \
	cd tests && NODE_URL=$$NODE_URL go test -v -count=1 -timeout 20m ./... ; \
	CODE=$$? ; \
	/tmp/interstellar-network stop 2>/dev/null || true ; \
	exit $$CODE

stop:
	/tmp/interstellar-network stop 2>/dev/null || true

status:
	/tmp/interstellar-network status 2>/dev/null || echo "No running network"

clean: stop
	rm -f /tmp/interstellar-network /tmp/interstellar-network.json

lint:
	cd network && golangci-lint run --timeout=10m --config=../.golangci.yml
	cd tests && golangci-lint run --timeout=10m --config=../.golangci.yml
