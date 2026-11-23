package dtcg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRefSiblingsOverride: keys beside a $ref in a source are merged over
// the referenced file, not dropped (R1-M10).
func TestRefSiblingsOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "base.tokens.json"), []byte(`{
  "color": {
    "bg": {"$type": "color", "$value": {"colorSpace": "srgb", "components": [0, 0, 0], "hex": "#000000"}},
    "fg": {"$type": "color", "$value": {"colorSpace": "srgb", "components": [1, 1, 1], "hex": "#ffffff"}}
  }
}`), 0o644)
	resolver := []byte(`{
  "version": "2025.10",
  "sets": {
    "base": {
      "sources": [
        {
          "$ref": "base.tokens.json",
          "color": {"bg": {"$type": "color", "$value": {"colorSpace": "srgb", "components": [1, 1, 1], "hex": "#ffffff"}}}
        }
      ]
    }
  },
  "resolutionOrder": [{"$ref": "#/sets/base"}]
}`)
	r, err := ParseResolver(resolver, dir)
	if err != nil {
		t.Fatal(err)
	}
	toks, err := r.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	bg := toks.Get("color.bg")
	if bg == nil {
		t.Fatal("no color.bg")
	}
	if hex := bg.Value.(*Object).String("hex"); hex != "#ffffff" {
		t.Errorf("color.bg = %s, want the local override #ffffff", hex)
	}
	if fg := toks.Get("color.fg"); fg == nil {
		t.Error("the referenced file's other tokens were lost")
	}
}
