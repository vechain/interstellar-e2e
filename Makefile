.PHONY: build-network ethersjs-deps web3js-deps viem-deps test clean stop status lint

build-network:
	cd network && go build -o /tmp/interstellar-network github.com/vechain/interstellar-e2e/network && cd ..

ethersjs-deps:
	@[ -d tests/eth_rpc/ethersjs/node_modules ] || (cd tests/eth_rpc/ethersjs && npm ci)

web3js-deps:
	@[ -d tests/eth_rpc/web3js/node_modules ] || (cd tests/eth_rpc/web3js && npm ci)

viem-deps:
	@[ -d tests/eth_rpc/viem/node_modules ] || (cd tests/eth_rpc/viem && npm ci)

test: build-network ethersjs-deps web3js-deps viem-deps
	@/tmp/interstellar-network start & \
	START_PID=$$! ; \
	trap '/tmp/interstellar-network stop 2>/dev/null || true; kill $$START_PID 2>/dev/null || true' EXIT ; \
	if ! NODE_URL=$$(/tmp/interstellar-network node-url); then \
		echo "ERROR: network never became ready (node-url timed out or failed) — aborting instead of reporting a false pass"; exit 1; \
	fi ; \
	if ! NODE_P2P_PORT=$$(/tmp/interstellar-network node-p2p-port); then \
		echo "ERROR: network never became ready (node-p2p-port timed out or failed) — aborting instead of reporting a false pass"; exit 1; \
	fi ; \
	if [ -z "$$NODE_URL" ] || [ -z "$$NODE_P2P_PORT" ]; then \
		echo "ERROR: empty node connection details — refusing to run e2e tests against no network"; exit 1; \
	fi ; \
	cd tests && NODE_URL=$$NODE_URL NODE_P2P_PORT=$$NODE_P2P_PORT go test -v -count=1 -timeout 20m ./...

stop:
	/tmp/interstellar-network stop 2>/dev/null || true

status:
	/tmp/interstellar-network status 2>/dev/null || echo "No running network"

clean: stop
	rm -f /tmp/interstellar-network /tmp/interstellar-network.json

lint:
	cd network && golangci-lint run --timeout=10m --config=../.golangci.yml
	cd tests && golangci-lint run --timeout=10m --config=../.golangci.yml
