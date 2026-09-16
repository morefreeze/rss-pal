package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	captcha "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/captcha/v20190722"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

// TencentCaptchaVerifier validates Tencent Captcha 2.0 tickets using the
// official SDK's TC3 signing implementation. Secrets never leave the server.
type TencentCaptchaVerifier struct {
	client       *captcha.Client
	appID        uint64
	appSecretKey string
	configured   bool
}

func NewTencentCaptchaVerifier(appID uint64, appSecretKey, secretID, secretKey string) *TencentCaptchaVerifier {
	p := profile.NewClientProfile()
	p.HttpProfile.Endpoint = "captcha.tencentcloudapi.com"
	p.HttpProfile.ReqTimeout = 10
	p.DisableRegionBreaker = true
	p.NetworkFailureMaxRetries = 0
	p.RateLimitExceededMaxRetries = 0
	p.UnsafeRetryOnConnectionFailure = false
	c, err := captcha.NewClient(common.NewCredential(secretID, secretKey), "", p)
	if err == nil {
		// The SDK does not expose CheckRedirect. Reject non-200 responses at
		// the transport boundary before net/http can follow a redirect.
		c.WithHttpTransport(captchaTransport{base: http.DefaultTransport})
	}
	return &TencentCaptchaVerifier{
		client: c, appID: appID, appSecretKey: appSecretKey,
		configured: err == nil && appID != 0 && strings.TrimSpace(appSecretKey) != "" && strings.TrimSpace(secretID) != "" && strings.TrimSpace(secretKey) != "",
	}
}

func (v *TencentCaptchaVerifier) Verify(ctx context.Context, token, ip string) error {
	if !v.configured || v.client == nil {
		return ErrVerificationUnavailable
	}
	if len(token) == 0 || len(token) > 2048 {
		return ErrVerificationRequired
	}
	var proof struct {
		Ticket  string `json:"ticket"`
		Randstr string `json:"randstr"`
	}
	if json.Unmarshal([]byte(token), &proof) != nil || strings.TrimSpace(proof.Ticket) == "" || strings.TrimSpace(proof.Randstr) == "" || strings.HasPrefix(proof.Ticket, "trerror") {
		return ErrVerificationRequired
	}
	request := captcha.NewDescribeCaptchaResultRequest()
	request.CaptchaType = common.Uint64Ptr(9)
	request.CaptchaAppId = common.Uint64Ptr(v.appID)
	request.AppSecretKey = common.StringPtr(v.appSecretKey)
	request.Ticket = common.StringPtr(proof.Ticket)
	request.Randstr = common.StringPtr(proof.Randstr)
	request.UserIp = common.StringPtr(ip)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := v.client.DescribeCaptchaResultWithContext(ctx, request)
	if err != nil || result == nil || result.Response == nil || result.Response.CaptchaCode == nil {
		return ErrVerificationUnavailable
	}
	if *result.Response.CaptchaCode != 1 {
		return ErrVerificationRequired
	}
	return nil
}

type captchaTransport struct{ base http.RoundTripper }

func (t captchaTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, ErrVerificationUnavailable
	}
	return response, nil
}
