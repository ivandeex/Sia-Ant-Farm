all: install

dependencies: get-all install-siad-dev install-std

get-all:
	go get -d ./...

lint-dependencies:
	go get -u golang.org/x/lint/golint
	./install-lint-dependencies.sh

count = 1

pkgs = \
	./ant \
	./antfarm \
	./cmd/sia-antfarm \
	./upnprouter \
	./version-test

release-pkgs = \
	./cmd/sia-antfarm

run = .

clean:
	rm -rf cover

cover: clean
	mkdir -p cover/
	@for package in $(pkgs); do \
		go test -covermode=atomic -coverprofile=cover/$$package.out ./$$package \
		&& go tool cover -html=cover/$$package.out -o=cover/$$package.html \
		&& rm cover/$$package.out ; \
	done

# fmt calls go fmt on all packages.
fmt:
	gofmt -s -l -w $(pkgs)

# install builds and installs binaries.
install:
	go install $(release-pkgs)

# install Sia Antfarm with debug option on (debug messages are printed to the log)
install-debug: install
	go build -o $(GOPATH)/bin/sia-antfarm-debug -tags='debug dev netgo' $(release-pkgs)

install-siad-dev:
	go build -o $(GOPATH)/bin/siad-dev -tags='dev' gitlab.com/NebulousLabs/Sia/cmd/siad

install-std:
	go install -race std

# markdown-spellcheck runs codespell on all markdown files that are not
# vendored.
markdown-spellcheck:
	git ls-files "*.md" :\!:"vendor/**" | xargs codespell --check-filenames

test: fmt vet install install-siad-dev
	go test -short -tags='debug testing netgo' -timeout=5s $(pkgs) -run="$(run)" -count=$(count)

test-long: clean fmt vet lint-ci install-siad-dev
	@mkdir -p cover
	go test --coverprofile='./cover/cover.out' -v -failfast -tags='testing debug netgo' -timeout=3600s $(pkgs) -run="$(run)" -count=$(count)

test-vlong: clean fmt vet lint-ci install-siad-dev
	@mkdir -p cover
	go test -v -tags='testing debug vlong netgo' -timeout=3600s $(pkgs) -run="$(run)" -count=$(count)

# Target to execute tests using 'dev' build tag, so that Sia Antfarm loads Sia
# dev constants.
test-vlong-dev: clean fmt vet lint-ci install-siad-dev
	@mkdir -p cover
	go test -v -tags='dev debug vlong netgo' -timeout=3600s $(pkgs) -run="$(run)" -count=$(count)

# Target to execute Foundation hardfork tests using 'foundation-antfarm-fix'
# branch and using 'dev' build tag, so that Sia Antfarm loads Sia dev
# constants.
test-vlong-foundation-dev: clean fmt vet lint-ci
	@mkdir -p cover
	go test -v -tags='dev debug vlong netgo' -timeout=3600s $(pkgs) -run="$(run)" -count=$(count)

# lint runs golangci-lint (which includes golint, a spellcheck of the codebase,
# and other linters), the custom analyzers, and also a markdown spellchecker.
lint: lint-dependencies markdown-spellcheck lint-golangci lint-analyze staticcheck

# lint-analyze runs the custom analyzers.
lint-analyze:
	analyze -lockcheck -- $(pkgs)

# lint-golangci runs golangci-lint analyzer.
lint-golangci:
	golangci-lint run -c .golangci.yml

# lint-ci runs golint.
lint-ci:
# golint is skipped on Windows.
ifneq ("$(OS)","Windows_NT")
# Linux
	go get golang.org/x/lint/golint
	golint -min_confidence=1.0 -set_exit_status $(pkgs)
endif

# spellcheck checks for misspelled words in comments or strings.
spellcheck: markdown-spellcheck
	golangci-lint run -c .golangci.yml -E misspell

# staticcheck runs the staticcheck tool
staticcheck:
	staticcheck $(pkgs)


# vet calls go vet on all packages.
# NOTE: go vet requires packages to be built in order to obtain type info.
vet:
	go vet $(pkgs)

.PHONY: all dependencies pkgs fmt vet install test lint clean cover
