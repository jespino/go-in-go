# make            build ./go-in-go
# make test       vet + integration tests (needs tinygo)
# make example    build the greeter module and run the example host
# make versions   build the test module once, run the tests with Go 1.22..1.26

GO_VERSIONS ?= go1.22.12 go1.23.12 go1.24.13 go1.25.9 local
KITCHEN := $(shell mktemp -u /tmp/kitchen-XXXXXX).gig

.PHONY: all go-in-go test example versions
all: go-in-go

go-in-go:
	go build -o go-in-go .

test: go-in-go
	go vet ./...
	go test ./... -count=1

example: go-in-go
	./go-in-go build examples/greeter
	go run ./examples/host examples/greeter/greeter.gig

versions: go-in-go
	./go-in-go build -o $(KITCHEN) test/testdata/kitchen
	@for v in $(GO_VERSIONS); do \
		printf "%-10s " $$v; \
		GIG_TEST_MODULE=$(KITCHEN) CGO_ENABLED=0 GOTOOLCHAIN=$$v go test ./test/ -count=1 | tail -1; \
	done
	rm -f $(KITCHEN)
