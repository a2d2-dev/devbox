package downloads

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	Offset       int64
	ETag         string
	LastModified string
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

func (p *HTTPProtocol) Schemes() []string { return []string{"http", "https"} }

func (p *HTTPProtocol) Download(ctx context.Context, in TransferRequest, progress func(TransferProgress)) (TransferResult, error) {
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 0}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return TransferResult{}, err
	}
	if in.Offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", in.Offset))
		if in.ETag != "" {
			req.Header.Set("If-Range", in.ETag)
		} else if in.LastModified != "" {
			req.Header.Set("If-Range", in.LastModified)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return TransferResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && in.Offset > 0 {
		if total := unsatisfiedRangeTotal(resp.Header.Get("Content-Range")); total == in.Offset {
			return TransferResult{Downloaded: in.Offset, Total: total, ResumeSupported: true, ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}, nil
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TransferResult{}, fmt.Errorf("remote server returned %s", resp.Status)
	}
	if in.Offset > 0 && resp.StatusCode == http.StatusPartialContent {
		if start := contentRangeStart(resp.Header.Get("Content-Range")); start != in.Offset {
			return TransferResult{}, fmt.Errorf("remote server returned an invalid Content-Range start %d for offset %d", start, in.Offset)
		}
	}

	offset := in.Offset
	flags := os.O_CREATE | os.O_WRONLY
	resume := offset > 0 && resp.StatusCode == http.StatusPartialContent
	if resume {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		offset = 0
	}
	f, err := os.OpenFile(in.PartialPath, flags, 0o640)
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
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
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
