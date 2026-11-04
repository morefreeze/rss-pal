package summaryeval

import (
	"encoding/json"
	"os"
	"testing"
)

type registryCase struct {
	ID          string `json:"id"`
	InputHash   string `json:"input_sha256"`
	TitleHash   string `json:"title_sha256"`
	ContentHash string `json:"content_sha256"`
	RubricHash  string `json:"rubric_sha256"`
}

type registryControl struct {
	ID   string `json:"id"`
	Hash string `json:"control_sha256"`
}

type registryCorpus struct {
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	FileHash    string            `json:"file_sha256"`
	SuiteHash   string            `json:"suite_sha256"`
	CLILoadable bool              `json:"cli_loadable"`
	Cases       []registryCase    `json:"cases"`
	Controls    []registryControl `json:"controls"`
}

func TestRegistryPinsBuiltinAndPrivateCorporaWithoutSourceText(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/operations/summary-eval-registry.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var registry struct {
		SchemaVersion int              `json:"schema_version"`
		Corpora       []registryCorpus `json:"corpora"`
	}
	if err := json.Unmarshal(raw, &registry); err != nil {
		t.Fatal(err)
	}
	if registry.SchemaVersion != 1 || len(registry.Corpora) != 3 {
		t.Fatal("registry must version all three corpus boundaries")
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	var check func(any)
	check = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				switch key {
				case "title", "content", "criteria", "source_evidence", "output", "brief", "detailed", "expected", "prohibited":
					t.Errorf("private content field in portable registry: %s", key)
				}
				check(child)
			}
		case []any:
			for _, child := range v {
				check(child)
			}
		}
	}
	check(decoded)
	corpora := map[string]registryCorpus{}
	for _, corpus := range registry.Corpora {
		corpora[corpus.ID] = corpus
		if !validSHA256(corpus.FileHash) {
			t.Fatal("missing exact corpus file hash")
		}
		for _, c := range corpus.Cases {
			if c.ID == "" || !validSHA256(c.InputHash) || !validSHA256(c.TitleHash) || !validSHA256(c.ContentHash) || !validSHA256(c.RubricHash) {
				t.Fatal("incomplete source/rubric binding")
			}
		}
		for _, c := range corpus.Controls {
			if c.ID == "" || !validSHA256(c.Hash) {
				t.Fatal("incomplete control binding")
			}
		}
	}
	suite, err := LoadSuite()
	if err != nil {
		t.Fatal(err)
	}
	builtin := corpora["builtin-9-v3"]
	if builtin.Version != suite.Version || builtin.FileHash != hashText(string(fixtureJSON)) || builtin.SuiteHash != suite.Hash() || !builtin.CLILoadable || len(builtin.Cases) != 9 || len(builtin.Controls) != 7 {
		t.Fatal("embedded default source/version binding changed")
	}
	for i, c := range suite.Cases {
		b, _ := json.Marshal(c.Criteria)
		entry := builtin.Cases[i]
		if entry.ID != c.ID || entry.InputHash != c.InputHash() || entry.TitleHash != hashText(c.Title) || entry.ContentHash != hashText(c.Content) || entry.RubricHash != hashText(string(b)) {
			t.Fatalf("built-in registry drift for %s", c.ID)
		}
	}
	for i, c := range suite.Controls {
		b, _ := json.Marshal(c)
		if builtin.Controls[i].ID != c.ID || builtin.Controls[i].Hash != hashText(string(b)) {
			t.Fatalf("built-in control registry drift for %s", c.ID)
		}
	}
	private := corpora["private-transfer-17-v3"]
	if private.Version != "summary-fidelity-transfer-proposal-2026-11-04-v3" || private.FileHash != "03b9d11cddf47e31067063aa3cdd6bfa44fd6bfc27b34da8e9c444f54b44dd7a" || private.SuiteHash != "07e186f1238e92da91495e91ce9f99e4403135e50838cf11652568b8a1723806" || !private.CLILoadable || len(private.Cases) != 17 || len(private.Controls) != 17 {
		t.Fatal("private 17-case freeze binding changed")
	}
	inputs := map[string]string{}
	for _, c := range private.Cases {
		inputs[c.ID] = c.InputHash
	}
	for _, c := range builtin.Cases {
		if inputs[c.ID] != c.InputHash {
			t.Fatalf("original nine generator inputs changed for %s", c.ID)
		}
	}
	full := corpora["private-full-source-7-v1"]
	if full.Version != "full-article-regression-v1" || full.FileHash != "8dca54322a0e254a574e156bc037bebeecc8d3a2280b96a78351cd9516622a75" || full.CLILoadable || len(full.Cases) != 7 || len(full.Controls) != 0 {
		t.Fatal("full-source manual boundary or manifest binding changed")
	}
}
