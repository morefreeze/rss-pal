package api_test

import (
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/registrationpolicy"
	"testing"
)

func TestPublicShareRegistrationPolicy(t *testing.T) {
	for _, tc := range []struct {
		mode, owners string
		want         bool
	}{
		{"selected_owners", "1", true}, {"selected_owners", "2", false}, {"selected_owners", "", false}, {"all", "", true}, {"disabled", "1", false},
	} {
		t.Run(tc.mode+tc.owners, func(t *testing.T) {
			f := newShareAPIFixtureWithOptions(t, api.ShareHandlerOptions{RegistrationPolicy: registrationpolicy.Parse(tc.mode, tc.owners)})
			const id = "0123456789abcdef0123456789abcdef"
			f.seedShortCode(t, id, "o6XKeCZTDZ22")
			for _, path := range []string{"/api/s/o6XKeCZTDZ22", "/api/share/" + f.signer.Sign(id)} {
				w := f.publicRequest("GET", path)
				var data struct {
					Allowed *bool `json:"registration_allowed"`
				}
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
					t.Fatal(err)
				}
				if data.Allowed == nil || *data.Allowed != tc.want {
					t.Fatal(w.Body.String())
				}
				assertNoPrivateJSONKeys(t, w.Body.Bytes())
			}
		})
	}
}
