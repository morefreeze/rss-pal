package api

import (
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/sharetoken"
	"strings"
)

func (h *AuthHandler) registrationShareSource(ref string) (repository.ShareRegistrationSource, error) {
	if strings.HasPrefix(ref, "s:") && sharetoken.IsValidShortCode(strings.TrimPrefix(ref, "s:")) {
		return repository.ShareRegistrationSource{ShortCode: strings.TrimPrefix(ref, "s:")}, nil
	}
	if strings.HasPrefix(ref, "t:") {
		signer, err := sharetoken.NewSigner(h.cfg.Share.Secret)
		if err != nil {
			return repository.ShareRegistrationSource{}, repository.ErrInvalidRegistrationShare
		}
		value, legacy, err := signer.Parse(strings.TrimPrefix(ref, "t:"))
		if err == nil {
			if legacy {
				return repository.ShareRegistrationSource{LegacyDigest: sharetoken.LegacyDigest(value)}, nil
			}
			return repository.ShareRegistrationSource{PublicID: value}, nil
		}
	}
	return repository.ShareRegistrationSource{}, repository.ErrInvalidRegistrationShare
}
