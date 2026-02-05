# Makefile for iox
#
# iox is a library with no external dependencies.
# Standard Go toolchain is sufficient.

.DEFAULT_GOAL := test

.PHONY: test
test:
	go test -race -covermode=atomic -coverprofile=coverage.out ./...

.PHONY: bench
bench:
	go test -bench=. -benchmem -run=^$$ ./...

.PHONY: bench-cpu
bench-cpu:
	go test -bench=. -benchmem -cpuprofile=cpu.out -run=^$$ ./...

.PHONY: bench-mem
bench-mem:
	go test -bench=. -benchmem -memprofile=mem.out -run=^$$ ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -f coverage.out cpu.out mem.out

.PHONY: help
help:
	@echo "iox Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  test      Run tests with race detection and coverage (default)"
	@echo "  bench     Run benchmarks"
	@echo "  bench-cpu Run benchmarks with CPU profiling (cpu.out)"
	@echo "  bench-mem Run benchmarks with memory profiling (mem.out)"
	@echo "  vet       Run go vet"
	@echo "  clean     Remove coverage.out, cpu.out, mem.out"
	@echo "  help      Show this help"
