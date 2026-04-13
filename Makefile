BINARY := biograph
BUILD_DIR := ./build
CMD_PATH := ./cmd/biograph

.PHONY: build run clean test tidy

build:
	CGO_ENABLED=1 go build -o $(BUILD_DIR)/$(BINARY) $(CMD_PATH)

run: build
	$(BUILD_DIR)/$(BINARY) $(ARGS)

clean:
	rm -rf $(BUILD_DIR) biograph.db biograph.bleve

test:
	go test ./...

tidy:
	go mod tidy

# Quick help
help: build
	$(BUILD_DIR)/$(BINARY) --help

# Ingest a PDF (usage: make ingest PDF=lecture.pdf COURSE=deep_learning)
ingest: build
	$(BUILD_DIR)/$(BINARY) ingest $(PDF) --course $(COURSE) $(if $(EXAM),--exam-date $(EXAM),)

# Ask a question
ask: build
	$(BUILD_DIR)/$(BINARY) ask "$(Q)"

# Search the graph
search: build
	$(BUILD_DIR)/$(BINARY) search "$(TERM)"

# Show status
status: build
	$(BUILD_DIR)/$(BINARY) status
