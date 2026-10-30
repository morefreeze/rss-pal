package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	ses "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ses/v20201002"
)

type sesEmailSender struct {
	client        *ses.Client
	ready         bool
	from, replyTo string
	templateID    uint64
}

func newSESEmailSender(a config.AuthConfig) *sesEmailSender {
	p := profile.NewClientProfile()
	p.HttpProfile.Endpoint = "ses.tencentcloudapi.com"
	p.HttpProfile.ReqTimeout = 15
	p.DisableRegionBreaker = true
	p.NetworkFailureMaxRetries = 0
	p.RateLimitExceededMaxRetries = 0
	p.UnsafeRetryOnConnectionFailure = false
	client, err := ses.NewClient(common.NewCredential(a.SESSecretID, a.SESSecretKey), a.SESRegion, p)
	if err == nil {
		client.WithHttpTransport(newTencentAuthTransport())
	}
	templateID, templateErr := strconv.ParseUint(a.SESTemplateID, 10, 64)
	_, fromErr := mail.ParseAddress(a.SESFrom)
	_, replyErr := mail.ParseAddress(a.SESReplyTo)
	return &sesEmailSender{client: client, from: a.SESFrom, replyTo: a.SESReplyTo, templateID: templateID,
		ready: err == nil && strings.TrimSpace(a.SESSecretID) != "" && strings.TrimSpace(a.SESSecretKey) != "" && strings.TrimSpace(a.SESRegion) != "" && templateErr == nil && templateID > 0 && fromErr == nil && (a.SESReplyTo == "" || replyErr == nil)}
}

func (s *sesEmailSender) Send(ctx context.Context, email, code string) error {
	if !s.ready || s.client == nil {
		return errors.New("email delivery unavailable")
	}
	data, _ := json.Marshal(map[string]string{"code": code})
	request := ses.NewSendEmailRequest()
	request.FromEmailAddress = common.StringPtr(s.from)
	request.Destination = common.StringPtrs([]string{email})
	request.Subject = common.StringPtr("RSS Pal 账号验证码")
	if s.replyTo != "" {
		request.ReplyToAddresses = common.StringPtr(s.replyTo)
	}
	request.Template = &ses.Template{TemplateID: common.Uint64Ptr(s.templateID), TemplateData: common.StringPtr(string(data))}
	request.TriggerType = common.Uint64Ptr(1)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, err := s.client.SendEmailWithContext(ctx, request)
	if err != nil || result == nil || result.Response == nil || result.Response.MessageId == nil || strings.TrimSpace(*result.Response.MessageId) == "" {
		return errors.New("email delivery unavailable")
	}
	return nil
}
