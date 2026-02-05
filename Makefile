# Makefile for iox
#
# iox is a library with no external dependencies.
# Standard Go toolchain is sufficient.

.DEFAULT_GOAL := test

.PHONY: test
test:
	go test -race -covermode=atomic -coverprofile=coverage.out ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -f coverage.out

.PHONY: help
help:
	@echo "iox Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  test   Run tests with race detection and coverage (default)"
	@echo "  vet    Run go vet"
	@echo "  clean  Remove coverage.out"
	@echo "  help   Show this help"
