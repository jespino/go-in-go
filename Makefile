# make            build ./go-in-go
# make test       vet + integration tests (needs tinygo)
# make example    build the greeter module and run the example host
# make goplugin   build and run the hashicorp/go-plugin examples (examples/goplugin)
# make versions   build the test module once, run the tests with Go 1.22..1.26
# make site       build the website (site/) into site/public/
# make site-serve preview the website at http://localhost:1313/go-in-go/

HUGO ?= hugo

GO_VERSIONS ?= go1.22.12 go1.23.12 go1.24.13 go1.25.9 local
KITCHEN := $(shell mktemp -u /tmp/kitchen-XXXXXX).gig

.PHONY: all go-in-go test example goplugin versions site site-api site-serve clean
all: go-in-go

go-in-go:
	go build -o go-in-go .

test: go-in-go
	go vet ./...
	go test ./... -count=1

example: go-in-go
	./go-in-go build examples/greeter
	go run ./examples/host examples/greeter/greeter.gig

goplugin: go-in-go
	./examples/goplugin/run.sh

versions: go-in-go
	./go-in-go build -o $(KITCHEN) test/testdata/kitchen
	@for v in $(GO_VERSIONS); do \
		printf "%-10s " $$v; \
		GIG_TEST_MODULE=$(KITCHEN) CGO_ENABLED=0 GOTOOLCHAIN=$$v go test ./test/ -count=1 | tail -1; \
	done
	rm -f $(KITCHEN)

site-api:
	go run github.com/jespino/tinyguest/cmd/apidoc -src . -import github.com/jespino/go-in-go \
		-skip assets,examples,internal,site,test \
		-desc "The gig package: load go-in-go modules and call them, generated from its doc comments." \
		-intro "Generated from the doc comments. Host programs import it; generated bindings are built on it." \
		-out site/content/api

site: site-api
	$(HUGO) --source site --minify --gc

site-serve: site-api
	$(HUGO) server --source site

clean:
	rm -rf go-in-go site/public site/resources site/content/api site/.hugo_build.lock examples/*/*.gig examples/goplugin/*/*/*.gig
