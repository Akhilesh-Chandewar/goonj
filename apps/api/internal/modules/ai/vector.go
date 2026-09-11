package ai

import (
	"strconv"
	"strings"
)

// VectorLiteral renders a float32 slice as pgvector's text representation
// ("[1,2,3]"). Cast the result at the call site with ::vector — pgx encodes
// []float32 as a float4[] array, which Postgres will not coerce into a
// vector column implicitly.
func VectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
