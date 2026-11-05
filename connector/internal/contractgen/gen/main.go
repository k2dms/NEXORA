package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/k2dms/nexora/connector/internal/contractgen"
)

func main() {
	out := flag.String("out", ".", "directory to write contract_gen.go into")
	flag.Parse()

	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "nexora-gencontract:", err)
		os.Exit(1)
	}
	src, err := contractgen.Source(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nexora-gencontract:", err)
		os.Exit(1)
	}
	path := filepath.Join(*out, "contract_gen.go")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "nexora-gencontract:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes) from %s\n", path, len(src), filepath.Join(root, "packages", "schema", "protocol"))
}
