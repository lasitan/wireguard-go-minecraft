PREFIX ?= /usr
DESTDIR ?=
BINDIR ?= $(PREFIX)/bin
export GO111MODULE := on

all: wireguard-go

MAKEFLAGS += --no-print-directory

wireguard-go: $(wildcard *.go) $(wildcard */*.go) .github/build/version.txt
	go build -v -o "$@"

install: wireguard-go
	@install -v -d "$(DESTDIR)$(BINDIR)" && install -v -m 0755 "$<" "$(DESTDIR)$(BINDIR)/wireguard-go"

test:
	go test ./...

clean:
	rm -f wireguard-go

.PHONY: all clean test install
