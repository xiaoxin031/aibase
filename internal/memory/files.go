package memory

import (
	"fmt"
	"io"
	"os"
)

func readFile(path string) ([]byte, error) {
	f, e := openRegular(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	const limit = 16 << 20
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if len(b) > limit {
		return nil, fmt.Errorf("memory file exceeds 16 MiB read budget; use bounded history search")
	}
	return b, e
}
func openRead(path string) (*os.File, error) { return openRegular(path) }
