package youtuberelay

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"sync"
	"time"
)

const (
	initialProbeBytes = int64(1 << 20)
	maxProbeBytes     = int64(4 << 20)
)

var singleRangePattern = regexp.MustCompile(`^bytes=(?:[0-9]+-[0-9]*|-[0-9]+)$`)

type Resolver interface {
	Resolve(ctx context.Context, videoID string) (ResolvedMedia, error)
}

type StartRequest struct {
	UserID    int
	ArticleID int
	VideoID   string
}

type Playback struct {
	Ticket             string
	Mode               string
	Quality            int
	ProgressiveQuality int
	ExpiresAt          time.Time
	HasProgressive     bool
}

type StreamKind string

const (
	StreamVideo       StreamKind = "video"
	StreamAudio       StreamKind = "audio"
	StreamProgressive StreamKind = "progressive"
)

type ServiceOptions struct {
	Resolver        Resolver
	Client          *http.Client
	MaxSessions     int
	IdleTTL         time.Duration
	AbsoluteTTL     time.Duration
	Now             func() time.Time
	UpstreamAllowed func(string) bool
}

type Service struct {
	resolver        Resolver
	client          *http.Client
	maxSessions     int
	idleTTL         time.Duration
	absoluteTTL     time.Duration
	now             func() time.Time
	upstreamAllowed func(string) bool

	mu       sync.Mutex
	sessions map[string]*relaySession
	byOwner  map[string]string
	pending  int

	stop      chan struct{}
	closeOnce sync.Once
}

type relaySession struct {
	ticket     string
	request    StartRequest
	resolved   ResolvedMedia
	manifest   []byte
	mode       string
	quality    int
	createdAt  time.Time
	lastAccess time.Time
	refreshed  bool
}

func NewService(options ServiceOptions) *Service {
	maxSessions := options.MaxSessions
	if maxSessions <= 0 {
		maxSessions = 2
	}
	idleTTL := options.IdleTTL
	if idleTTL <= 0 {
		idleTTL = 10 * time.Minute
	}
	absoluteTTL := options.AbsoluteTTL
	if absoluteTTL <= 0 {
		absoluteTTL = 6 * time.Hour
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          20,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}}
	}
	allowed := options.UpstreamAllowed
	if allowed == nil {
		allowed = safeMediaURL
	}
	service := &Service{
		resolver:        options.Resolver,
		client:          client,
		maxSessions:     maxSessions,
		idleTTL:         idleTTL,
		absoluteTTL:     absoluteTTL,
		now:             now,
		upstreamAllowed: allowed,
		sessions:        make(map[string]*relaySession),
		byOwner:         make(map[string]string),
		stop:            make(chan struct{}),
	}
	go service.cleanupLoop()
	return service
}

func (s *Service) Start(ctx context.Context, request StartRequest) (Playback, error) {
	if s.resolver == nil {
		return Playback{}, ErrResolveFailed
	}
	now := s.now()
	key := ownerKey(request.UserID, request.ArticleID)

	s.mu.Lock()
	s.cleanupExpiredLocked(now)
	if ticket := s.byOwner[key]; ticket != "" {
		if session := s.sessions[ticket]; session != nil {
			session.lastAccess = now
			playback := s.playbackLocked(session)
			s.mu.Unlock()
			return playback, nil
		}
	}
	if len(s.sessions)+s.pending >= s.maxSessions {
		s.mu.Unlock()
		return Playback{}, ErrCapacity
	}
	s.pending++
	s.mu.Unlock()

	inserted := false
	defer func() {
		if inserted {
			return
		}
		s.mu.Lock()
		s.pending--
		s.mu.Unlock()
	}()

	resolved, err := s.resolver.Resolve(ctx, request.VideoID)
	if err != nil {
		return Playback{}, err
	}
	if !s.selectionAllowed(resolved.Selection) {
		return Playback{}, ErrNoCompatibleMedia
	}
	ticket, err := newTicket()
	if err != nil {
		return Playback{}, fmt.Errorf("%w: ticket", ErrResolveFailed)
	}

	session := &relaySession{
		ticket:     ticket,
		request:    request,
		resolved:   resolved,
		createdAt:  now,
		lastAccess: now,
		quality:    resolved.Selection.Quality,
	}
	if resolved.Selection.Video != nil && resolved.Selection.Audio != nil {
		videoRanges, videoErr := s.probeFormat(ctx, *resolved.Selection.Video)
		audioRanges, audioErr := s.probeFormat(ctx, *resolved.Selection.Audio)
		if videoErr == nil && audioErr == nil {
			manifest, manifestErr := GenerateMPD(
				ticket,
				resolved.Info.Duration,
				resolved.Selection,
				videoRanges,
				audioRanges,
			)
			if manifestErr != nil {
				return Playback{}, manifestErr
			}
			session.mode = "dash"
			session.manifest = manifest
		} else if resolved.Selection.Progressive == nil {
			return Playback{}, fmt.Errorf("%w: mp4 index probe", ErrUpstream)
		}
	}
	if session.mode == "" {
		if resolved.Selection.Progressive == nil {
			return Playback{}, ErrNoCompatibleMedia
		}
		session.mode = "progressive"
		session.quality = resolved.Selection.Progressive.Height
	}

	s.mu.Lock()
	s.pending--
	s.sessions[ticket] = session
	s.byOwner[key] = ticket
	inserted = true
	playback := s.playbackLocked(session)
	s.mu.Unlock()
	return playback, nil
}

func (s *Service) Manifest(ticket string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.cleanupExpiredLocked(now)
	session := s.sessions[ticket]
	if session == nil || len(session.manifest) == 0 {
		return nil, ErrSessionNotFound
	}
	session.lastAccess = now
	return append([]byte(nil), session.manifest...), nil
}

func (s *Service) Open(
	ctx context.Context,
	method string,
	ticket string,
	kind StreamKind,
	rangeHeader string,
	ifRange string,
) (*http.Response, error) {
	if method != http.MethodGet && method != http.MethodHead {
		return nil, ErrUpstream
	}
	if rangeHeader != "" && !singleRangePattern.MatchString(rangeHeader) {
		return nil, ErrInvalidRange
	}

	format, err := s.sessionFormat(ticket, kind)
	if err != nil {
		return nil, err
	}
	response, err := s.openUpstream(ctx, method, format, rangeHeader, ifRange)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusGone {
		return response, nil
	}
	_ = response.Body.Close()
	if err := s.refresh(ctx, ticket); err != nil {
		return nil, err
	}
	format, err = s.sessionFormat(ticket, kind)
	if err != nil {
		return nil, err
	}
	return s.openUpstream(ctx, method, format, rangeHeader, ifRange)
}

func (s *Service) Close() {
	s.closeOnce.Do(func() { close(s.stop) })
}

func (s *Service) probeFormat(ctx context.Context, format Format) (MP4IndexRanges, error) {
	prefix, err := s.fetchPrefix(ctx, format, initialProbeBytes)
	if err != nil {
		return MP4IndexRanges{}, err
	}
	ranges, err := ParseMP4IndexRanges(prefix)
	if !errors.Is(err, ErrMP4Incomplete) {
		return ranges, err
	}
	prefix, err = s.fetchPrefix(ctx, format, maxProbeBytes)
	if err != nil {
		return MP4IndexRanges{}, err
	}
	return ParseMP4IndexRanges(prefix)
}

func (s *Service) fetchPrefix(ctx context.Context, format Format, size int64) ([]byte, error) {
	if !s.upstreamAllowed(format.URL) {
		return nil, ErrUpstream
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, format.URL, nil)
	if err != nil {
		return nil, ErrUpstream
	}
	applyUpstreamHeaders(request, format)
	request.Header.Set("Range", fmt.Sprintf("bytes=0-%d", size-1))
	request.Header.Set("Accept-Encoding", "identity")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("%w: probe status %d", ErrUpstream, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, size+1))
	if err != nil {
		return nil, fmt.Errorf("%w: probe read", ErrUpstream)
	}
	if int64(len(body)) > size {
		return nil, fmt.Errorf("%w: probe exceeds limit", ErrUpstream)
	}
	return body, nil
}

func (s *Service) openUpstream(
	ctx context.Context,
	method string,
	format Format,
	rangeHeader string,
	ifRange string,
) (*http.Response, error) {
	if !s.upstreamAllowed(format.URL) {
		return nil, ErrUpstream
	}
	request, err := http.NewRequestWithContext(ctx, method, format.URL, nil)
	if err != nil {
		return nil, ErrUpstream
	}
	applyUpstreamHeaders(request, format)
	request.Header.Set("Accept-Encoding", "identity")
	if rangeHeader != "" {
		request.Header.Set("Range", rangeHeader)
	}
	if ifRange != "" {
		request.Header.Set("If-Range", ifRange)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	return response, nil
}

func applyUpstreamHeaders(request *http.Request, format Format) {
	for _, name := range []string{"User-Agent", "Referer", "Origin"} {
		if value := format.HTTPHeaders[name]; value != "" {
			request.Header.Set(name, value)
		}
	}
}

func (s *Service) sessionFormat(ticket string, kind StreamKind) (Format, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.cleanupExpiredLocked(now)
	session := s.sessions[ticket]
	if session == nil {
		return Format{}, ErrSessionNotFound
	}
	session.lastAccess = now
	var format *Format
	switch kind {
	case StreamVideo:
		format = session.resolved.Selection.Video
	case StreamAudio:
		format = session.resolved.Selection.Audio
	case StreamProgressive:
		format = session.resolved.Selection.Progressive
	default:
		return Format{}, ErrSessionNotFound
	}
	if format == nil {
		return Format{}, ErrSessionNotFound
	}
	return *format, nil
}

func (s *Service) refresh(ctx context.Context, ticket string) error {
	s.mu.Lock()
	session := s.sessions[ticket]
	if session == nil || session.refreshed {
		s.mu.Unlock()
		return ErrUpstream
	}
	session.refreshed = true
	videoID := session.request.VideoID
	videoFormatID := formatID(session.resolved.Selection.Video)
	audioFormatID := formatID(session.resolved.Selection.Audio)
	progressiveFormatID := formatID(session.resolved.Selection.Progressive)
	s.mu.Unlock()

	resolved, err := s.resolver.Resolve(ctx, videoID)
	if err != nil {
		return err
	}
	video := findFormat(resolved.Info.Formats, videoFormatID)
	audio := findFormat(resolved.Info.Formats, audioFormatID)
	progressive := findFormat(resolved.Info.Formats, progressiveFormatID)
	if (videoFormatID != "" && video == nil) ||
		(audioFormatID != "" && audio == nil) ||
		(progressiveFormatID != "" && progressive == nil) {
		return ErrResolveFailed
	}
	if (video != nil && !s.upstreamAllowed(video.URL)) ||
		(audio != nil && !s.upstreamAllowed(audio.URL)) ||
		(progressive != nil && !s.upstreamAllowed(progressive.URL)) {
		return ErrUpstream
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	session = s.sessions[ticket]
	if session == nil {
		return ErrSessionNotFound
	}
	session.resolved.Info = resolved.Info
	session.resolved.Selection.Video = video
	session.resolved.Selection.Audio = audio
	session.resolved.Selection.Progressive = progressive
	session.lastAccess = s.now()
	return nil
}

func (s *Service) selectionAllowed(selection Selection) bool {
	for _, format := range []*Format{selection.Video, selection.Audio, selection.Progressive} {
		if format != nil && !s.upstreamAllowed(format.URL) {
			return false
		}
	}
	return selection.Progressive != nil || (selection.Video != nil && selection.Audio != nil)
}

func (s *Service) playbackLocked(session *relaySession) Playback {
	progressiveQuality := 0
	if session.resolved.Selection.Progressive != nil {
		progressiveQuality = session.resolved.Selection.Progressive.Height
	}
	return Playback{
		Ticket:             session.ticket,
		Mode:               session.mode,
		Quality:            session.quality,
		ProgressiveQuality: progressiveQuality,
		ExpiresAt:          session.createdAt.Add(s.absoluteTTL),
		HasProgressive:     session.resolved.Selection.Progressive != nil,
	}
}

func (s *Service) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			s.cleanupExpiredLocked(s.now())
			s.mu.Unlock()
		case <-s.stop:
			return
		}
	}
}

func (s *Service) cleanupExpiredLocked(now time.Time) {
	for ticket, session := range s.sessions {
		if now.Sub(session.lastAccess) <= s.idleTTL &&
			now.Sub(session.createdAt) <= s.absoluteTTL {
			continue
		}
		delete(s.sessions, ticket)
		delete(s.byOwner, ownerKey(session.request.UserID, session.request.ArticleID))
	}
}

func ownerKey(userID, articleID int) string {
	return fmt.Sprintf("%d:%d", userID, articleID)
}

func newTicket() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func formatID(format *Format) string {
	if format == nil {
		return ""
	}
	return format.ID
}

func findFormat(formats []Format, id string) *Format {
	if id == "" {
		return nil
	}
	for _, format := range formats {
		if format.ID == id {
			value := format
			return &value
		}
	}
	return nil
}
