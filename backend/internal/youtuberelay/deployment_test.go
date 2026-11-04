package youtuberelay

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	potProviderVersion = "1.3.1"
	potProviderSHA256  = "b8ceec7f76143da172aaf5ebeec0c2d218e5680c063b931586bca48567069b38"
	potProviderPath    = "/root/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip"
	potProviderAssets  = "../../build_assets/bgutil-ytdlp-pot-provider/"
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
		"COPY build_assets/bgutil-ytdlp-pot-provider/bgutil-ytdlp-pot-provider-${BGUTIL_PROVIDER_VERSION}.zip " + potProviderPath,
		"COPY build_assets/bgutil-ytdlp-pot-provider/LICENSE /root/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.LICENSE",
		"sha256sum -c -",
	}
	for _, value := range requiredDockerfile {
		if !strings.Contains(dockerfile, value) {
			t.Fatalf("backend/Dockerfile missing %q", value)
		}
	}
	// A missing local version must fail the COPY, without an online fallback.
	start := strings.Index(dockerfile, "ARG BGUTIL_PROVIDER_VERSION=")
	install := strings.SplitN(dockerfile[start:], "WORKDIR /app", 2)[0]
	for _, networkInput := range []string{"wget", "curl", "https://", "http://"} {
		if strings.Contains(install, networkInput) {
			t.Fatalf("POT provider installation must use only local build inputs, found %q", networkInput)
		}
	}
}

func TestPOTProviderVendoredArchiveContainsPinnedSource(t *testing.T) {
	body, err := os.ReadFile(potProviderAssets + "bgutil-ytdlp-pot-provider-" + potProviderVersion + ".zip")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != potProviderSHA256 {
		t.Fatalf("vendored archive SHA-256 = %s, want %s", got, potProviderSHA256)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		sources = append(sources, entry.Name)
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		source, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name, err)
		}
		if len(source) == 0 {
			t.Fatalf("empty source file %s", entry.Name)
		}
		if strings.HasSuffix(entry.Name, "/getpot_bgutil.py") && !bytes.Contains(source, []byte("__version__ = '"+potProviderVersion+"'")) {
			t.Fatalf("provider source does not declare pinned version %s", potProviderVersion)
		}
	}
	slices.Sort(sources)
	want := []string{
		"yt_dlp_plugins/extractor/getpot_bgutil.py",
		"yt_dlp_plugins/extractor/getpot_bgutil_http.py",
		"yt_dlp_plugins/extractor/getpot_bgutil_script.py",
	}
	if !slices.Equal(sources, want) {
		t.Fatalf("archive source files = %v, want %v", sources, want)
	}
}

func TestPOTProviderVendoredLicenseIsVerbatim(t *testing.T) {
	license := mustReadDeploymentFile(t, potProviderAssets+"LICENSE")
	const upstreamLicenseSHA256 = "3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(license))); got != upstreamLicenseSHA256 {
		t.Fatalf("upstream GPLv3 license SHA-256 = %s, want %s", got, upstreamLicenseSHA256)
	}
}

func TestPOTProviderBuildChecksumFailsClosed(t *testing.T) {
	dockerfile := mustReadDeploymentFile(t, "../../Dockerfile")
	var checksumCommand string
	for _, instruction := range strings.Split(strings.ReplaceAll(dockerfile, "\\\n", ""), "\n") {
		if strings.HasPrefix(instruction, "RUN ") && strings.Contains(instruction, potProviderPath) && strings.Contains(instruction, "sha256sum -c -") {
			checksumCommand = strings.TrimPrefix(instruction, "RUN ")
		}
	}
	if checksumCommand == "" || strings.Contains(checksumCommand, "wget") || strings.Contains(checksumCommand, "curl") {
		t.Fatal("POT provider build must have an offline checksum RUN instruction")
	}
	archive, err := os.ReadFile(potProviderAssets + "bgutil-ytdlp-pot-provider-" + potProviderVersion + ".zip")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		body    []byte
		digest  string
		missing bool
		wantOK  bool
	}{
		{name: "exact archive", body: archive, digest: potProviderSHA256, wantOK: true},
		{name: "corrupted archive", body: append([]byte("corrupt"), archive...), digest: potProviderSHA256},
		{name: "wrong checksum", body: archive, digest: strings.Repeat("0", 64)},
		{name: "missing archive", digest: potProviderSHA256, missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "provider.zip")
			if !tc.missing {
				if err := os.WriteFile(path, tc.body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			command := strings.ReplaceAll(checksumCommand, potProviderPath, path)
			cmd := exec.Command("sh", "-c", command)
			cmd.Env = append(os.Environ(), "BGUTIL_PROVIDER_SHA256="+tc.digest)
			output, err := cmd.CombinedOutput()
			if (err == nil) != tc.wantOK {
				t.Fatalf("checksum command error = %v, want success %v: %s", err, tc.wantOK, output)
			}
		})
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
