package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

var errRequiresOmarchy = errors.New("requires an Omarchy machine (ID=omarchy in /etc/os-release)")
var osReleasePath = "/etc/os-release"

func requireOmarchy() error {
	file, err := os.Open(osReleasePath)
	if err != nil {
		return errRequiresOmarchy
	}
	defer file.Close()
	if !isOmarchyRelease(file) {
		return errRequiresOmarchy
	}
	return nil
}

func isOmarchyRelease(input io.Reader) bool {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if found && key == "ID" {
			return strings.Trim(value, `"'`) == "omarchy"
		}
	}
	return false
}
