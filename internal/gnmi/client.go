// Package gnmi wraps the openconfig gnmic API client for read-only gNMI
// Get and Capabilities requests against an EOS device.
package gnmi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	gnmiapi "github.com/openconfig/gnmic/pkg/api"
	gtarget "github.com/openconfig/gnmic/pkg/api/target"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/x86taka/arista-eos-mcp/internal/config"
)

// Client wraps a gnmic Target.
type Client struct {
	addr     string
	username string
	password string
	target   *gtarget.Target
}

// New creates a gNMI target for the device. The gRPC connection is established
// lazily on the first request. timeout is the per-request timeout.
func New(dev *config.Device, timeout time.Duration) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", dev.Host, dev.GNMIPort)
	opts := []gnmiapi.TargetOption{
		gnmiapi.Address(addr),
		gnmiapi.Username(dev.Username),
		gnmiapi.Password(dev.Password),
		gnmiapi.Timeout(timeout),
	}
	if dev.GNMIInsecureOrDefault() {
		opts = append(opts, gnmiapi.Insecure(true))
	} else {
		if dev.TLSSkipVerifyOrDefault() {
			opts = append(opts, gnmiapi.SkipVerify(true))
		}
		if dev.GNMITLSCA != "" {
			opts = append(opts, gnmiapi.TLSCA(dev.GNMITLSCA))
		}
	}

	t, err := gnmiapi.NewTarget(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to build gNMI target: %w", err)
	}
	return &Client{addr: addr, username: dev.Username, password: dev.Password, target: t}, nil
}

func (c *Client) connect(ctx context.Context) error {
	if err := c.target.CreateGNMIClient(ctx); err != nil {
		return fmt.Errorf("gNMI connect to %s failed: %w", c.addr, err)
	}
	return nil
}

// Get issues a gNMI Get for the given paths. encoding is one of
// "json", "json_ietf", "ascii", "proto" (default "json_ietf"); dataType is one
// of "all", "config", "state", "operational" (default "all"). The response is
// returned as protojson-encoded bytes.
func (c *Client) Get(ctx context.Context, paths []string, encoding, dataType string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("at least one path is required")
	}
	if encoding == "" {
		encoding = "json_ietf"
	}
	if dataType == "" {
		dataType = "all"
	}

	if err := c.connect(ctx); err != nil {
		return "", err
	}

	opts := []gnmiapi.GNMIOption{
		gnmiapi.Encoding(encoding),
		gnmiapi.DataType(dataType),
	}
	for _, p := range paths {
		opts = append(opts, gnmiapi.Path(p))
	}

	req, err := gnmiapi.NewGetRequest(opts...)
	if err != nil {
		return "", fmt.Errorf("failed to build gNMI Get request: %w", err)
	}

	resp, err := c.target.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("gNMI Get failed: %w", err)
	}

	out, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("failed to encode gNMI response: %w", err)
	}
	return string(out), nil
}

// Capabilities returns the device gNMI capabilities as protojson bytes.
func (c *Client) Capabilities(ctx context.Context) (string, error) {
	if err := c.connect(ctx); err != nil {
		return "", err
	}
	resp, err := c.target.Capabilities(ctx)
	if err != nil {
		return "", fmt.Errorf("gNMI Capabilities failed: %w", err)
	}
	out, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("failed to encode gNMI capabilities: %w", err)
	}
	return string(out), nil
}

// Subscribe opens a gNMI subscription for the given paths and collects updates
// for the bounded window duration, then returns them as a text report.
//
// mode selects the subscription style:
//   - "once": request the current values once and return immediately
//   - "sample": stream samples every sampleInterval for the window duration
//   - "on-change" (default): stream updates as values change for the window
//
// maxUpdates caps the number of notifications collected (0 = default 500).
func (c *Client) Subscribe(ctx context.Context, paths []string, mode string, sampleInterval, window time.Duration, maxUpdates int) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("at least one path is required")
	}
	if mode == "" {
		mode = "on-change"
	}
	if sampleInterval <= 0 {
		sampleInterval = 10 * time.Second
	}
	if window <= 0 {
		window = 15 * time.Second
	}
	if maxUpdates <= 0 {
		maxUpdates = 500
	}
	if err := c.connect(ctx); err != nil {
		return "", err
	}

	// Build the subscription request.
	opts := []gnmiapi.GNMIOption{gnmiapi.EncodingJSON_IETF()}
	if mode == "once" {
		opts = append(opts, gnmiapi.SubscriptionListModeONCE())
	} else {
		opts = append(opts, gnmiapi.SubscriptionListModeSTREAM())
	}
	for _, p := range paths {
		subOpts := []gnmiapi.GNMIOption{gnmiapi.Path(p)}
		switch mode {
		case "sample":
			subOpts = append(subOpts, gnmiapi.SubscriptionModeSAMPLE(), gnmiapi.SampleInterval(sampleInterval))
		case "once":
			// no per-subscription mode needed for ONCE
		default: // on-change
			subOpts = append(subOpts, gnmiapi.SubscriptionModeON_CHANGE())
		}
		opts = append(opts, gnmiapi.Subscription(subOpts...))
	}
	req, err := gnmiapi.NewSubscribeRequest(opts...)
	if err != nil {
		return "", fmt.Errorf("failed to build gNMI Subscribe request: %w", err)
	}

	// Bound the whole operation by the window and attach credentials, which the
	// gnmic Target normally adds for unary RPCs.
	subCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	if c.username != "" {
		subCtx = metadata.AppendToOutgoingContext(subCtx, "username", c.username)
	}
	if c.password != "" {
		subCtx = metadata.AppendToOutgoingContext(subCtx, "password", c.password)
	}

	stream, err := c.target.Client.Subscribe(subCtx)
	if err != nil {
		return "", fmt.Errorf("gNMI Subscribe to %s failed: %w", c.addr, err)
	}
	if err := stream.Send(req); err != nil {
		return "", fmt.Errorf("failed to send gNMI Subscribe request: %w", err)
	}

	var b strings.Builder
	marshal := protojson.MarshalOptions{Multiline: true, Indent: "  "}
	count, synced := 0, false
	for count < maxUpdates {
		resp, rerr := stream.Recv()
		if rerr != nil {
			// A deadline/cancel or EOF marks the normal end of the window.
			if errors.Is(rerr, io.EOF) || subCtx.Err() != nil {
				break
			}
			return b.String(), fmt.Errorf("gNMI subscription stream error: %w", rerr)
		}
		if resp.GetResponse() == nil {
			continue
		}
		if resp.GetSyncResponse() {
			synced = true
			b.WriteString("--- sync_response (initial state delivered) ---\n")
			if mode == "once" {
				break
			}
			continue
		}
		if upd := resp.GetUpdate(); upd != nil {
			out, merr := marshal.Marshal(upd)
			if merr != nil {
				continue
			}
			fmt.Fprintf(&b, "--- update #%d ---\n%s\n", count+1, string(out))
			count++
		}
	}

	header := fmt.Sprintf("gNMI %s subscription on %s: collected %d update(s) over %s (synced=%v)\n\n",
		mode, c.addr, count, window, synced)
	return header + b.String(), nil
}
