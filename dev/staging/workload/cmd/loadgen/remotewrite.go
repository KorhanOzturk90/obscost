package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/golang/snappy"
)

// Remote write 1.0 (prometheus/prompb WriteRequest), encoded by hand. The
// message is four fields deep and fixed, and hand-encoding it keeps this
// module to two dependencies instead of pulling in prometheus/prometheus.
//
//	WriteRequest { repeated TimeSeries timeseries = 1; }
//	TimeSeries   { repeated Label labels = 1; repeated Sample samples = 2; }
//	Label        { string name = 1; string value = 2; }
//	Sample       { double value = 1; int64 timestamp = 2; }

type label struct{ name, value string }

// series is one time series; labels must already be sorted by name, which
// remote write requires (__name__ sorts first among lower-case names).
type series struct {
	labels []label
	value  float64
}

func appendVarint(b []byte, v uint64) []byte { return binary.AppendUvarint(b, v) }

func appendTag(b []byte, field, wire int) []byte { return appendVarint(b, uint64(field<<3|wire)) }

func appendBytesField(b []byte, field int, payload []byte) []byte {
	b = appendTag(b, field, 2)
	b = appendVarint(b, uint64(len(payload)))
	return append(b, payload...)
}

func appendStringField(b []byte, field int, s string) []byte {
	b = appendTag(b, field, 2)
	b = appendVarint(b, uint64(len(s)))
	return append(b, s...)
}

// encodeWriteRequest encodes every series with one sample at tsMillis.
func encodeWriteRequest(ss []series, tsMillis int64) []byte {
	out := make([]byte, 0, len(ss)*128)
	var ts, lbl, smp []byte
	for _, s := range ss {
		ts = ts[:0]
		for _, l := range s.labels {
			lbl = lbl[:0]
			lbl = appendStringField(lbl, 1, l.name)
			lbl = appendStringField(lbl, 2, l.value)
			ts = appendBytesField(ts, 1, lbl)
		}
		smp = smp[:0]
		smp = appendTag(smp, 1, 1) // double: fixed64
		smp = binary.LittleEndian.AppendUint64(smp, math.Float64bits(s.value))
		smp = appendTag(smp, 2, 0) // int64: varint
		smp = appendVarint(smp, uint64(tsMillis))
		ts = appendBytesField(ts, 2, smp)
		out = appendBytesField(out, 1, ts)
	}
	return out
}

type pushError struct {
	status int
	body   string
}

func (e *pushError) Error() string { return fmt.Sprintf("push: HTTP %d: %s", e.status, e.body) }

// retryable is true for errors worth retrying: transport errors, 429 and
// 5xx. A 4xx is Mimir rejecting the data itself (a limit, a bad label),
// and sending the same bytes again cannot change the answer.
func retryable(err error) bool {
	pe, ok := err.(*pushError)
	if !ok {
		return true
	}
	return pe.status == http.StatusTooManyRequests || pe.status >= 500
}

type pusher struct {
	client  *http.Client
	url     string
	tenant  string
	headers map[string]string
}

func (p *pusher) push(ctx context.Context, ss []series, tsMillis int64) error {
	body := snappy.Encode(nil, encodeWriteRequest(ss, tsMillis))
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * 250 * time.Millisecond):
			}
		}
		err = p.once(ctx, body)
		if err == nil || !retryable(err) {
			return err
		}
	}
	return err
}

func (p *pusher) once(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("X-Scope-OrgID", p.tenant)
	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return &pushError{status: resp.StatusCode, body: string(bytes.TrimSpace(msg))}
}
