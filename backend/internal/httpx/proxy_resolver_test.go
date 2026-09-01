package httpx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProxyFallbackResolverReplacesSyntheticDNSAnswer(t *testing.T) {
	system := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("198.18.0.127")}}, nil
	}}
	trusted := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("185.199.108.133")}}, nil
	}}

	got, err := (proxyFallbackResolver{system: system, trusted: trusted}).LookupIPAddr(t.Context(), "raw.githubusercontent.com")
	if err != nil {
		t.Fatalf("LookupIPAddr: %v", err)
	}
	if len(got) != 1 || !got[0].IP.Equal(net.ParseIP("185.199.108.133")) {
		t.Fatalf("resolved addresses = %v, want trusted public address", got)
	}
}

func TestProxyFallbackClientPinsTrustedAddress(t *testing.T) {
	system := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("198.18.0.127")}}, nil
	}}
	trusted := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("185.199.108.133")}}, nil
	}}
	proxyURL, err := url.Parse("http://127.0.0.1:8888")
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 1)
	proxyDialer := &recordingDialer{dial: func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			reader := bufio.NewReader(server)
			var raw strings.Builder
			for {
				line, readErr := reader.ReadString('\n')
				if readErr != nil {
					return
				}
				raw.WriteString(line)
				if line == "\r\n" {
					requests <- raw.String()
					break
				}
			}
			_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		}()
		return client, nil
	}}
	client := newClientWithProxyFallback(time.Second, system, trusted, proxyDialer, func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	})

	response, err := client.Get("http://raw.githubusercontent.com/feed")
	if err != nil {
		t.Fatalf("proxied request failed: %v", err)
	}
	response.Body.Close()
	if request := <-requests; !strings.Contains(request, "GET http://185.199.108.133:80/feed HTTP/1.1\r\n") {
		t.Fatalf("proxy request did not use trusted address: %q", request)
	}
}

func TestDoHResolverReturnsPublicAAnswers(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		if got := request.URL.Query().Get("name"); got != "raw.githubusercontent.com" {
			t.Fatalf("DoH name = %q", got)
		}
		w.Header().Set("Content-Type", "application/dns-json")
		fmt.Fprint(w, `{"Status":0,"Answer":[{"name":"raw.githubusercontent.com","type":5,"data":"example.invalid"},{"name":"raw.githubusercontent.com","type":1,"data":"185.199.108.133"}]}`)
	}))
	defer server.Close()

	resolver := &dohResolver{endpoint: server.URL, client: server.Client()}
	got, err := resolver.LookupIPAddr(t.Context(), "raw.githubusercontent.com")
	if err != nil {
		t.Fatalf("LookupIPAddr: %v", err)
	}
	if len(got) != 1 || !got[0].IP.Equal(net.ParseIP("185.199.108.133")) {
		t.Fatalf("resolved addresses = %v, want public A answer", got)
	}
	if _, err := resolver.LookupIPAddr(t.Context(), "raw.githubusercontent.com"); err != nil {
		t.Fatalf("cached LookupIPAddr: %v", err)
	}
	if requests != 1 {
		t.Fatalf("DoH requests = %d, want one cached lookup", requests)
	}
}

func TestDoHResolverBoundsSuccessfulHostCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/dns-json")
		fmt.Fprint(w, `{"Status":0,"Answer":[{"type":1,"data":"185.199.108.133"}]}`)
	}))
	defer server.Close()

	resolver := &dohResolver{endpoint: server.URL, client: server.Client()}
	const expectedCapacity = 256
	for index := 0; index <= expectedCapacity; index++ {
		host := fmt.Sprintf("host-%03d.example", index)
		if _, err := resolver.LookupIPAddr(t.Context(), host); err != nil {
			t.Fatalf("LookupIPAddr(%q): %v", host, err)
		}
	}
	if got := len(resolver.cache); got > expectedCapacity {
		t.Fatalf("DoH cache entries = %d, want at most %d", got, expectedCapacity)
	}
}

func TestSyntheticDNSLiteralIsNeverAnOutboundTarget(t *testing.T) {
	const rawURL = "http://198.18.0.127/feed"
	if _, err := ValidateURL(rawURL); err == nil {
		t.Fatal("ValidateURL accepted a synthetic DNS literal")
	}

	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pinRequest(request, net.DefaultResolver); err == nil {
		t.Fatal("pinRequest accepted a synthetic DNS literal")
	}

	dialed := false
	dial := safeDialContext(net.DefaultResolver, &recordingDialer{dial: func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("unexpected dial")
	}})
	if _, err := dial(t.Context(), "tcp", "198.18.0.127:80"); err == nil {
		t.Fatal("safeDialContext accepted a synthetic DNS literal")
	}
	if dialed {
		t.Fatal("synthetic DNS literal reached the network dialer")
	}
}
