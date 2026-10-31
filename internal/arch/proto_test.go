package arch

import (
	"strings"
	"testing"
)

func TestGeneratedProtoIsImportedOnlyInsideHostAPI(t *testing.T) {
	graph := importGraph(t)
	const generatedPrefix = modulePrefix + "internal/hostapi/gen/"
	const hostAPIPrefix = modulePrefix + "internal/hostapi/"
	for from, imports := range graph {
		if strings.HasPrefix(from, hostAPIPrefix) {
			continue
		}
		for _, imp := range imports {
			if strings.HasPrefix(imp, generatedPrefix) {
				t.Errorf("proto boundary: %s imports generated package %s; generated protocol types stay inside internal/hostapi", short(from), short(imp))
			}
		}
	}
}
