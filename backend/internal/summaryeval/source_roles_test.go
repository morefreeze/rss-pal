package summaryeval

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllRoutesKeepEveryClassRubricControlAndProvenanceOutOfGenerator(t *testing.T) {
	suite := fiveClassSuite(t)
	ids := map[string]string{}
	for _, c := range suite.Cases {
		ids[c.ID] = "CASE_ID_ONLY_" + c.ID
	}
	var selected []string
	for i := range suite.Cases {
		c := &suite.Cases[i]
		c.ID = ids[c.ID]
		if c.Source.BasedOn != "" {
			c.Source.BasedOn = ids[c.Source.BasedOn]
		}
		c.Source.EvidenceRef = "PROVENANCE_ONLY_EVIDENCE"
		c.Source.Note = "PROVENANCE_ONLY_NOTE"
		for j := range c.Criteria {
			c.Criteria[j].Expected += " RUBRIC_ONLY_EXPECTED"
			c.Criteria[j].Prohibited = append(c.Criteria[j].Prohibited, "RUBRIC_ONLY_PROHIBITED")
		}
		if c.Split == "train" {
			selected = append(selected, c.ID)
		}
	}
	for i := range suite.Controls {
		suite.Controls[i].ID = "CONTROL_ONLY_ID_" + suite.Controls[i].ID
		suite.Controls[i].CaseID = ids[suite.Controls[i].CaseID]
		suite.Controls[i].Output = Output{Brief: "CONTROL_ONLY_BRIEF", Detailed: "CONTROL_ONLY_DETAIL"}
	}
	image := filepath.Join(t.TempDir(), "source.jpg")
	if err := os.WriteFile(image, []byte("test-only image bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.CaseIDs, cfg.MaxCalls = selected, 128
	cfg.Routes = []Route{Default, Stream, Custom, CustomStream, Vision, VisionStream}
	cfg.ImagePaths = []string{image}
	calls := 0
	a, err := Run(context.Background(), cfg, suite, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		for _, forbidden := range []string{"CASE_ID_ONLY_", "PROVENANCE_ONLY_", "RUBRIC_ONLY_", "CONTROL_ONLY_", "\"failure_class\"", "\"criteria\"", "\"based_on\""} {
			if strings.Contains(string(body), forbidden) {
				t.Errorf("generator received private role data: %s", forbidden)
			}
		}
		if strings.Contains(string(body), `"stream":true`) {
			rsp := wireResponse("data: {\"choices\":[{\"delta\":{\"content\":\"仅由输入生成的摘要\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n")
			rsp.Header.Set("Content-Type", "text/event-stream")
			return rsp, nil
		}
		return wireResponse(`{"choices":[{"message":{"content":"仅由输入生成的摘要"},"finish_reason":"stop"}]}`), nil
	}))
	if err != nil || a.Status != "ungraded" || len(a.Results) != len(selected)*6 || calls != len(selected)*12 {
		t.Fatalf("source-role route coverage incomplete: err=%v calls=%d", err, calls)
	}
}
