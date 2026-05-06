package billing

import (
	"fmt"
	"math/rand/v2"
)

// newID generates a short random ID with a given prefix, e.g. "t_a3f9b2c1".
func newID(prefix string) string {
	return fmt.Sprintf("%s_%08x", prefix, rand.Uint32())
}
