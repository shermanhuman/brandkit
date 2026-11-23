package dtcg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestConformanceSuite runs the DTCG 2025.10 test suite: every positive
// case must parse and resolve, every negative case must fail.
func TestConformanceSuite(t *testing.T) {
	for _, m := range []string{"format", "resolver"} {
		dir := filepath.Join("..", "..", "testdata", "dtcg-test-suite", "2025.10", m)
		var man struct {
			Tests []struct{ ID, Type, Input, Purpose string }
		}
		b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &man); err != nil {
			t.Fatal(err)
		}
		if len(man.Tests) == 0 {
			t.Fatalf("%s: no tests", m)
		}
		for _, tc := range man.Tests {
			t.Run(m+"/"+tc.ID, func(t *testing.T) {
				data, err := os.ReadFile(filepath.Join(dir, tc.Input))
				if err != nil {
					t.Fatal(err)
				}
				if m == "format" {
					_, err = ParseDocument(data)
				} else {
					err = resolveEveryPermutation(data, dir)
				}
				switch tc.Type {
				case "PositiveEvaluationTest":
					if err != nil {
						t.Errorf("%s (%s): %v", tc.Input, tc.Purpose, err)
					}
				case "NegativeEvaluationTest":
					if err == nil {
						t.Errorf("%s accepted; want an error: %s", tc.Input, tc.Purpose)
					}
				default:
					t.Fatalf("unknown test type %q", tc.Type)
				}
			})
		}
	}
}

func resolveEveryPermutation(data []byte, dir string) error {
	r, err := ParseResolver(data, dir)
	if err != nil {
		return err
	}
	inputs := []map[string]string{{}}
	for _, m := range r.Modifiers() {
		var next []map[string]string
		for _, in := range inputs {
			for _, c := range m.Contexts {
				n := map[string]string{m.Name: c}
				for k, v := range in {
					n[k] = v
				}
				next = append(next, n)
			}
		}
		inputs = next
	}
	for _, in := range inputs {
		if _, err := r.Resolve(in); err != nil {
			return err
		}
	}
	return nil
}
