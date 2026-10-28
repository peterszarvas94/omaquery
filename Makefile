PREFIX ?= $(HOME)/.local

.PHONY: build test check install
build:
	go build -trimpath -o build/omaquery ./cmd/omaquery

test:
	go test -race ./...

check: test
	go vet ./...
	omarchy plugin validate .
	qmllint -I "$${OMARCHY_PATH:-/usr/share/omarchy}/shell" Popup.qml

install: build
	install -Dm755 build/omaquery "$(PREFIX)/bin/omaquery"
	"$(PREFIX)/bin/omaquery" install
