package httpx

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDirectBufferedGETIgnoresProxyAndRejectsPrivateRedirect(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.invalid:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "public response", true: "private redirect"}[redirect], func(t *testing.T) {
			client := NewDirectBufferedGETClient(time.Second)
			transport := client.Transport.(*bufferedGETTransport)
			transport.base.resolver = fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
			}}
			dials := &recordingDialer{dial: func(_ context.Context, _, _ string) (net.Conn, error) {
				client, server := net.Pipe()
				go func() {
					defer server.Close()
					if _, err := http.ReadRequest(bufio.NewReader(server)); err != nil {
						return
					}
					if redirect {
						_, _ = io.WriteString(server, "HTTP/1.1 302 Found\r\nLocation: http://127.0.0.1/private\r\nContent-Length: 0\r\n\r\n")
					} else {
						_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
					}
				}()
				return client, nil
			}}
			transport.base.dialer = dials
			response, err := client.Get("http://safe.example/item")
			if redirect {
				if err == nil || !strings.Contains(err.Error(), "blocked address") {
					t.Fatalf("want blocked redirect, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil || string(body) != "ok" {
					t.Fatalf("body=%q err=%v", body, err)
				}
			}
			if addresses := dials.addresses(); len(addresses) != 1 || addresses[0] != "93.184.216.34:80" {
				t.Fatalf("unexpected dial destinations: %v", addresses)
			}
			if _, err := client.Get("http://169.254.169.254/latest/meta-data/"); err == nil {
				t.Fatal("accepted private target")
			}
			if len(dials.addresses()) != 1 {
				t.Fatal("dialed a blocked address")
			}
		})
	}
}
