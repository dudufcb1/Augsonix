package provider

import (
	"path/filepath"
	"strings"
	"testing"
)

const testPNGDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func TestSaveImagesForTextModelWritesFileAndNotesPath(t *testing.T) {
	t.Chdir(t.TempDir())
	got := SaveImagesForTextModel("hola", []string{testPNGDataURL})
	if !strings.Contains(got, "hola") {
		t.Fatalf("expected original content preserved, got %q", got)
	}
	if !strings.Contains(got, "read_image") {
		t.Fatalf("expected read_image hint, got %q", got)
	}
	matches, err := filepath.Glob(filepath.Join(".reasonix", "attachments", "read-image-*.png"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one saved image in cwd, got %v (err %v)", matches, err)
	}
	if !strings.Contains(got, filepath.ToSlash(matches[0])) {
		t.Fatalf("expected note to mention %q, got %q", matches[0], got)
	}
}

func TestSaveImagesForTextModelFallsBackSilently(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := SaveImagesForTextModel("texto", nil); got != "texto" {
		t.Fatalf("no images: expected input unchanged, got %q", got)
	}
	if got := SaveImagesForTextModel("texto", []string{"not-a-data-url"}); got != "texto" {
		t.Fatalf("bad data url: expected input unchanged, got %q", got)
	}
	if got := SaveImagesForTextModel("texto", []string{"data:text/plain;base64,aGVsbG8="}); got != "texto" {
		t.Fatalf("non-image mime: expected input unchanged, got %q", got)
	}
}
