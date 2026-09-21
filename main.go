package main

import (
	"fmt"

	"github.com/sergklm98/kopia-lib/repo/compression"
	"github.com/sergklm98/kopia-lib/repo/logging"
)

func main() {
	fmt.Println("kopia-server")
	fmt.Println("gzip supported:", compression.IsSupported("gzip"))
	_ = logging.Module("server")
}
