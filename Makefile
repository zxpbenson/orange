.PHONY: all build run clean fmt install setup-config build-linux build-mac build-windows

APP_NAME = orange
BUILD_DIR = build
CONFIG_DIR = ~/.config/$(APP_NAME)
CONFIG_FILE = $(CONFIG_DIR)/config.json

all: build

build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) main.go
	@echo "Building mcpserver..."
	go build -o $(BUILD_DIR)/mcpserver cmd/mcpserver/main.go

build-linux:
	@echo "Building $(APP_NAME) for Linux..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 main.go
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/mcpserver-linux-amd64 cmd/mcpserver/main.go

build-mac:
	@echo "Building $(APP_NAME) for MacOS (Apple Silicon)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(APP_NAME)-darwin-arm64 main.go
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/mcpserver-darwin-arm64 cmd/mcpserver/main.go
	@echo "Building $(APP_NAME) for MacOS (Intel)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(APP_NAME)-darwin-amd64 main.go
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/mcpserver-darwin-amd64 cmd/mcpserver/main.go

build-windows:
	@echo "Building $(APP_NAME) for Windows..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe main.go
	GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/mcpserver-windows-amd64.exe cmd/mcpserver/main.go

run:
	@echo "Running $(APP_NAME)..."
	./$(BUILD_DIR)/$(APP_NAME)

clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)

fmt:
	@echo "Formatting code..."
	go fmt ./...

install:
	@echo "Installing $(APP_NAME) to $$GOPATH/bin..."
	go install

setup-config:
	@echo "Setting up example config..."
	mkdir -p $(CONFIG_DIR)
	cp config.example.json $(CONFIG_FILE)
	@echo "Config copied to $(CONFIG_FILE). Please edit it to add your API key."
