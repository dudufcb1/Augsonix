package claudesessions

import (
	"os"
	"testing"
	"time"
)

// TestProbeRealHomes es una sonda manual contra las sesiones de esta máquina.
// Solo corre con PROBE=1; sirve para ver el listado real, no para validar nada.
func TestProbeRealHomes(t *testing.T) {
	if os.Getenv("PROBE") == "" {
		t.Skip("sonda manual: PROBE=1")
	}
	homes := DiscoverHomes(os.Getenv("HOME"))
	t.Log("homes:", homes)
	start := time.Now()
	list, err := List(homes, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d sesiones en %v", len(list), time.Since(start))
	for i, s := range list {
		if i >= 8 {
			break
		}
		t.Logf("[%s] %s | %s | %s | %.1fMB | %s",
			s.HomeLabel, s.Title, s.Cwd, s.Branch, float64(s.Size)/1e6,
			s.Updated.Format("02 Jan 15:04"))
	}
}
