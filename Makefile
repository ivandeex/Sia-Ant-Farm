all: install

dependencies:
	go install -tags='dev' gitlab.com/NebulousLabs/Sia/cmd/siad
	go install -race std
	go get -u golang.org/x/lint/golint
	go get -d ./...
	./install-dependencies.sh

count = 1

pkgs = \
	./sia-antfarm \
	./ant

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
	go install $(pkgs)

install-siad-dev:
	go build -o $(GOPATH)/bin/siad-dev -tags='dev' gitlab.com/NebulousLabs/Sia/cmd/siad

# markdown-spellcheck runs codespell on all markdown files that are not
# vendored.
markdown-spellcheck:
	git ls-files "*.md" :\!:"vendor/**" | xargs codespell --check-filenames

test: fmt vet install install-siad-dev
	go test -short -tags='debug testing netgo' -timeout=5s $(pkgs) -run=$(run) -count=$(count)

test-long: clean fmt vet lint-ci install-siad-dev
	@mkdir -p cover
	go test --coverprofile='./cover/cover.out' -v -failfast -tags='testing debug netgo' -timeout=3600s $(pkgs) -run=$(run) -count=$(count)

# lint runs golangci-lint (which includes golint, a spellcheck of the codebase,
# and other linters), the custom analyzers, and also a markdown spellchecker.
lint: markdown-spellcheck lint-analyze staticcheck
	golangci-lint run -c .golangci.yml

# lint-analyze runs the custom analyzers.
lint-analyze:
	analyze -lockcheck -- $(pkgs)

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
# NOTE: this is not yet enabled in the CI system.
staticcheck:
	staticcheck $(pkgs)


# vet calls go vet on all packages.
# NOTE: go vet requires packages to be built in order to obtain type info.
vet:
	go vet $(pkgs)


.PHONY: all dependencies pkgs fmt vet install test lint clean cover
