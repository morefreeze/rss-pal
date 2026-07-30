package youtuberelay

import (
	"os"
	"strings"
	"testing"
)

func TestPOTProviderDeploymentIsPinnedAndInternal(t *testing.T) {
	compose := mustReadDeploymentFile(t, "../../../docker-compose.yml")
	dockerfile := mustReadDeploymentFile(t, "../../Dockerfile")

	requiredCompose := []string{
		"youtube-pot:",
		"image: brainicism/bgutil-ytdlp-pot-provider:1.3.1",
		"YOUTUBE_POT_PROVIDER_URL: http://youtube-pot:4416",
		"youtube-pot:\n        condition: service_started",
	}
	for _, value := range requiredCompose {
		if !strings.Contains(compose, value) {
			t.Fatalf("docker-compose.yml missing %q", value)
		}
	}
	if strings.Contains(compose, "4416:4416") {
		t.Fatal("youtube-pot must not publish its port to the host")
	}

	requiredDockerfile := []string{
		"BGUTIL_PROVIDER_VERSION=1.3.1",
		"BGUTIL_PROVIDER_SHA256=b8ceec7f76143da172aaf5ebeec0c2d218e5680c063b931586bca48567069b38",
		"/root/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip",
		"sha256sum -c -",
	}
	for _, value := range requiredDockerfile {
		if !strings.Contains(dockerfile, value) {
			t.Fatalf("backend/Dockerfile missing %q", value)
		}
	}
}

func mustReadDeploymentFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
