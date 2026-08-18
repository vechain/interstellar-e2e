.PHONY: build-network test test-all suites clean stop status lint

# Which suite(s) under tests/ to run. Empty = every suite.
#   make test SUITE=eip1153
#   make test SUITE="eip1153 eip2935"
#   make test-eip1153            (shorthand)
SUITE ?=
# Optional -run regexp: make test SUITE=eip1153 RUN=TestTransientStorage
RUN ?=
# -p 1: every suite shares one sender account, parallel packages fight over nonces.
TEST_FLAGS ?= -v -count=1 -p 1 -timeout 20m

ifeq ($(strip $(SUITE)),)
TEST_PKGS := ./...
else
TEST_PKGS := $(patsubst %,./%/,$(strip $(SUITE)))
endif

ifneq ($(strip $(RUN)),)
RUN_FLAG := -run '$(RUN)'
endif

# $(call run_tests,<go test package args>) — boot network, run tests, always tear down.
define run_tests
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
cd tests && NODE_URL=$$NODE_URL NODE_P2P_PORT=$$NODE_P2P_PORT go test $(TEST_FLAGS) $(RUN_FLAG) $(1)
endef

build-network:
	cd network && go build -o /tmp/interstellar-network github.com/vechain/interstellar-e2e/network && cd ..

test: build-network
	@for s in $(strip $(SUITE)); do \
		if [ ! -d "tests/$$s" ]; then \
			echo "ERROR: no such suite 'tests/$$s'. Available:"; $(MAKE) -s suites; exit 1; \
		fi ; \
	done
	$(call run_tests,$(TEST_PKGS))

# make test-eip1153 == make test SUITE=eip1153
test-%:
	@$(MAKE) test SUITE=$*

test-all:
	@$(MAKE) test SUITE=

suites:
	@find tests -mindepth 1 -maxdepth 1 -type d ! -name helper -exec basename {} \; | sort | sed 's/^/  /'

stop:
	/tmp/interstellar-network stop 2>/dev/null || true

status:
	/tmp/interstellar-network status 2>/dev/null || echo "No running network"

clean: stop
	rm -f /tmp/interstellar-network /tmp/interstellar-network.json

lint:
	cd network && golangci-lint run --timeout=10m --config=../.golangci.yml
	cd tests && golangci-lint run --timeout=10m --config=../.golangci.yml
