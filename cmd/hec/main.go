package main

import (
	"fmt"
	"os"

	"github.com/StealthEyeLLC/hec/internal/hec"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(hec.VersionText())
		return
	}

	fmt.Fprintln(os.Stderr, "usage: hec version")
	os.Exit(2)
}
