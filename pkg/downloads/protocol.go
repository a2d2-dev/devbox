package downloads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Protocol is the extension point for future transfer protocols. This issue
// registers only HTTPProtocol for http and https URLs.
type Protocol interface {
	Schemes() []string
	Download(context.Context, TransferRequest, func(TransferProgress)) (TransferResult, error)
}

type TransferRequest struct {
	URL          string
	PartialPath  string
	Root         *os.Root
	Offset       int64
	ETag         string
	LastModified string
	CanWrite     func() bool
}

type TransferProgress struct {
	Downloaded      int64
	Total           int64
	BytesReceived   int64
	ResumeSupported bool
	ETag            string
	LastModified    string
}

type TransferResult struct {
	Downloaded      int64
	Total           int64
	ResumeSupported bool
	ETag            string
	LastModified    string
}

type HTTPProtocol struct {
	Client *http.Client
}

func newDownloadHTTPClient(base *http.Client, allowPrivateNetworks bool) *http.Client {
	client := &http.Client{}
	if base != nil {
		*client = *base
	}
	var transport *http.Transport
	if configured, ok := client.Transport.(*http.Transport); ok {
		transport = configured.Clone()
		transport.DialContext = restrictedDialContext(configured.DialContext, allowPrivateNetworks)
	} else {
		transport = http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = restrictedDialContext(nil, allowPrivateNetworks)
	}
	transport.Proxy = nil
	transport.DialTLS = nil
	transport.DialTLSContext = nil
	client.Transport = transport

	configuredRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to unsupported scheme %q", req.URL.Scheme)
		}
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme == "http" {
			return errors.New("HTTPS redirect to HTTP is not allowed")
		}
		if configuredRedirect != nil {
			return configuredRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return client
}

func restrictedDialContext(baseDial func(context.Context, string, string) (net.Conn, error), allowPrivateNetworks bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if allowPrivateNetworks {
			if baseDial != nil {
				return baseDial(ctx, network, address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("validate download address: %w", err)
		}
		if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
			if isRestrictedDownloadIP(ip) {
				return nil, fmt.Errorf("private network address %s is not allowed", ip)
			}
			if baseDial != nil {
				return baseDial(ctx, network, address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}

		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve download host %q: %w", host, err)
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("resolve download host %q: no addresses", host)
		}
		for _, ip := range addresses {
			if isRestrictedDownloadIP(ip) {
				return nil, fmt.Errorf("private network address %s for host %q is not allowed", ip, host)
			}
		}
		if baseDial != nil {
			return baseDial(ctx, network, address)
		}
		var lastErr error
		for _, ip := range addresses {
			connection, dialErr := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
}

func isRestrictedDownloadIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified()
}

func (p *HTTPProtocol) Schemes() []string { return []string{"http", "https"} }

func (p *HTTPProtocol) Download(ctx context.Context, in TransferRequest, progress func(TransferProgress)) (TransferResult, error) {
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 0}
	}
	resume := in.Offset > 0 && requestValidator(in) != ""
	requestOffset := int64(0)
	if resume {
		requestOffset = in.Offset
	}
	resp, err := p.request(ctx, client, in, requestOffset)
	if err != nil {
		return TransferResult{}, err
	}

	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && resume {
		if total := unsatisfiedRangeTotal(resp.Header.Get("Content-Range")); total == in.Offset && responseValidatorMatches(resp, in) {
			_ = resp.Body.Close()
			return TransferResult{Downloaded: in.Offset, Total: total, ResumeSupported: true, ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}, nil
		}
		_ = resp.Body.Close()
		resp, err = p.request(ctx, client, in, 0)
		if err != nil {
			return TransferResult{}, err
		}
		requestOffset = 0
		resume = false
	} else if resp.StatusCode == http.StatusPartialContent && resume && !responseValidatorMatches(resp, in) {
		_ = resp.Body.Close()
		resp, err = p.request(ctx, client, in, 0)
		if err != nil {
			return TransferResult{}, err
		}
		requestOffset = 0
		resume = false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TransferResult{}, fmt.Errorf("remote server returned %s", resp.Status)
	}
	if resp.StatusCode == http.StatusPartialContent {
		if start := contentRangeStart(resp.Header.Get("Content-Range")); start != requestOffset {
			return TransferResult{}, fmt.Errorf("remote server returned an invalid Content-Range start %d for offset %d", start, requestOffset)
		}
	}

	offset := requestOffset
	flags := os.O_CREATE | os.O_WRONLY
	resume = offset > 0 && resp.StatusCode == http.StatusPartialContent
	if resume {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		offset = 0
	}
	flags |= unix.O_NOFOLLOW
	if in.CanWrite != nil && !in.CanWrite() {
		return TransferResult{}, context.Canceled
	}
	if in.Root == nil {
		return TransferResult{}, errors.New("download root is required")
	}
	f, err := in.Root.OpenFile(in.PartialPath, flags, 0o640)
	if err != nil {
		return TransferResult{}, err
	}
	defer f.Close()

	total := responseTotal(resp, offset)
	meta := TransferProgress{
		Downloaded: offset, Total: total, ResumeSupported: resp.StatusCode == http.StatusPartialContent || strings.Contains(strings.ToLower(resp.Header.Get("Accept-Ranges")), "bytes"),
		ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified"),
	}
	progress(meta)

	buf := make([]byte, 64*1024)
	downloaded := offset
	for {
		if in.CanWrite != nil && !in.CanWrite() {
			return TransferResult{}, context.Canceled
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if in.CanWrite != nil && !in.CanWrite() {
				return TransferResult{}, context.Canceled
			}
			written, writeErr := f.Write(buf[:n])
			if writeErr != nil {
				return TransferResult{}, writeErr
			}
			if written != n {
				return TransferResult{}, io.ErrShortWrite
			}
			downloaded += int64(n)
			meta.Downloaded = downloaded
			meta.BytesReceived = int64(n)
			progress(meta)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return TransferResult{}, readErr
		}
	}
	if err := f.Sync(); err != nil {
		return TransferResult{}, err
	}
	if total == 0 {
		total = downloaded
	}
	return TransferResult{Downloaded: downloaded, Total: total, ResumeSupported: meta.ResumeSupported, ETag: meta.ETag, LastModified: meta.LastModified}, nil
}

func (p *HTTPProtocol) request(ctx context.Context, client *http.Client, in TransferRequest, offset int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		req.Header.Set("If-Range", requestValidator(in))
	}
	return client.Do(req)
}

func requestValidator(in TransferRequest) string {
	if in.ETag != "" {
		return in.ETag
	}
	return in.LastModified
}

func responseValidatorMatches(resp *http.Response, in TransferRequest) bool {
	if in.ETag != "" {
		return resp.Header.Get("ETag") == in.ETag
	}
	return in.LastModified != "" && resp.Header.Get("Last-Modified") == in.LastModified
}

func responseTotal(resp *http.Response, offset int64) int64 {
	if resp.StatusCode == http.StatusPartialContent {
		if total := contentRangeTotal(resp.Header.Get("Content-Range")); total >= 0 {
			return total
		}
	}
	if resp.ContentLength >= 0 {
		return offset + resp.ContentLength
	}
	return 0
}

func contentRangeTotal(value string) int64 {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[1] == "*" {
		return -1
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return -1
	}
	return n
}

func contentRangeStart(value string) int64 {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "bytes ") {
		return -1
	}
	rangeAndTotal := strings.SplitN(strings.TrimPrefix(value, "bytes "), "/", 2)
	if len(rangeAndTotal) != 2 {
		return -1
	}
	bounds := strings.SplitN(rangeAndTotal[0], "-", 2)
	if len(bounds) != 2 {
		return -1
	}
	start, err := strconv.ParseInt(bounds[0], 10, 64)
	if err != nil || start < 0 {
		return -1
	}
	return start
}

func unsatisfiedRangeTotal(value string) int64 {
	if !strings.HasPrefix(value, "bytes */") {
		return -1
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(value, "bytes */"), 10, 64)
	if err != nil {
		return -1
	}
	return n
}
