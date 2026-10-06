# monixtend
APP      := monixtend
BIN      := bin/$(APP)
PKG      := ./cmd/$(APP)
VERSION  ?= 0.2.0-alpha

LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build run version test fmt vet install clean

# Default target: build the binary.
all: build

build:
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)
	@echo "built $(BIN) $(VERSION)"

# Start the server with the built binary. Add args like:
#   make run ARGS="--codec auto --port 9000"
run: build
	$(BIN) run $(ARGS)

# Host as an AirPlay Display receiver (macOS extends its desktop onto a monitor).
airplay: build
	$(BIN) airplay $(ARGS)

version: build
	$(BIN) version

test:
	go test ./...

fmt:
	gofmt -l .

vet:
	go vet ./...

install:
	go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

clean:
	rm -rf bin