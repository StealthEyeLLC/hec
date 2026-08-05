package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/StealthEyeLLC/hec/internal/hec"
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		printUsage()
		return 2
	}

	switch os.Args[1] {
	case "version":
		if len(os.Args) != 2 {
			printUsage()
			return 2
		}
		fmt.Println(hec.VersionText())
		return 0
	case "call":
		return runCall(os.Args[2:])
	case "serve":
		if len(os.Args) != 2 {
			printUsage()
			return 2
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := hec.Serve(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "hec serve:", err)
			return 1
		}
		return 0
	case "job-run":
		fmt.Fprintln(os.Stderr, "hec job-run is not implemented in Build Slice 1")
		return 2
	default:
		printUsage()
		return 2
	}
}

func runCall(arguments []string) int {
	if len(arguments) < 1 || len(arguments) > 2 {
		printUsage()
		return 2
	}

	rawArgs := "{}"
	if len(arguments) == 2 {
		rawArgs = arguments[1]
	}
	operationArgs := make(map[string]any)
	decoder := json.NewDecoder(bytesReader(rawArgs))
	decoder.UseNumber()
	if err := decoder.Decode(&operationArgs); err != nil {
		fmt.Fprintln(os.Stderr, "hec call: invalid arguments JSON:", err)
		return 2
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing JSON value")
		}
		fmt.Fprintln(os.Stderr, "hec call: invalid arguments JSON:", err)
		return 2
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	result := hec.NewDispatcher().Dispatch(ctx, hec.CallRequest{
		Operation: arguments[0],
		Args:      operationArgs,
	})
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "hec call: encode result:", err)
		return 1
	}
	if result.OK {
		return 0
	}
	return 1
}

func bytesReader(value string) io.Reader {
	return &stringReader{value: value}
}

type stringReader struct {
	value  string
	offset int
}

func (r *stringReader) Read(buffer []byte) (int, error) {
	if r.offset >= len(r.value) {
		return 0, io.EOF
	}
	count := copy(buffer, r.value[r.offset:])
	r.offset += count
	return count, nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  hec version")
	fmt.Fprintln(os.Stderr, "  hec call <operation> [args-json]")
	fmt.Fprintln(os.Stderr, "  hec serve")
	fmt.Fprintln(os.Stderr, "  hec job-run")
}
