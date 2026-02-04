.PHONY: build test agent manager clean

build:
	go build -o ./bin/agent ./agent/cmd
	go build -o ./bin/manager ./manager/cmd

test:
	go test ./...

agent:
	go run ./agent/cmd

manager:
	go run ./manager/cmd

clean:
	go clean -cache