PREFIX ?= $(HOME)/.local
PLUGIN_DIR ?= $(HOME)/.config/omarchy/plugins/omaquery.prompt

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
	install -Dm644 manifest.json "$(PLUGIN_DIR)/manifest.json"
	install -Dm644 Popup.qml "$(PLUGIN_DIR)/Popup.qml"
