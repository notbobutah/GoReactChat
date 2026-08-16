package service

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	chatv1 "github.com/expona-ai/lumi-go/server/gen/lumi/chat/v1"
	"github.com/expona-ai/lumi-go/server/internal/newsagent"
	"github.com/expona-ai/lumi-go/server/internal/newswatch"
)

// WatchNews subscribes the caller to the news watcher and streams every change
// until the client disconnects.
//
// The handler holds the stream open indefinitely and writes when the watcher
// has something to say. That inversion is the whole point: a scan takes about a
// minute, so the client cannot wait on a response, and polling would either
// miss the result or ask repeatedly for one that is not ready.
func (s *ChatService) WatchNews(
	ctx context.Context,
	_ *connect.Request[chatv1.WatchNewsRequest],
	stream *connect.ServerStream[chatv1.NewsEvent],
) error {
	if s.news == nil {
		return connect.NewError(connect.CodeUnimplemented,
			errors.New("the news watcher is not enabled on this deployment"))
	}

	events, cancel := s.news.Subscribe()
	defer cancel()

	// Subscribing is what makes a scan eligible to run: spend follows
	// attention, so an unopened page costs nothing. ErrTooSoon is the normal
	// case — the interval has not elapsed — and the snapshot already on its way
	// carries the existing digest, so there is nothing to report.
	if err := s.news.MaybeScan(ctx); err != nil && !errors.Is(err, newswatch.ErrTooSoon) {
		s.logger.Warn("news: scan not started", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			// The client hung up. Normal, and not worth logging.
			return nil

		case ev, ok := <-events:
			if !ok {
				return nil
			}
			msg := newsEvent(ev)
			if msg == nil {
				continue
			}
			if err := stream.Send(msg); err != nil {
				return err
			}
		}
	}
}

// newsEvent translates one watcher event into its wire form. Returns nil for an
// event with nothing to say, which the caller skips.
func newsEvent(ev newswatch.Event) *chatv1.NewsEvent {
	switch {
	case ev.Snapshot != nil:
		snap := &chatv1.NewsSnapshot{
			State:  scanState(ev.Snapshot.State),
			Digest: newsDigest(ev.Snapshot.Digest),
		}
		if !ev.Snapshot.NextScanAllowedAt.IsZero() {
			snap.NextScanAllowedUnix = ev.Snapshot.NextScanAllowedAt.Unix()
		}
		return &chatv1.NewsEvent{Event: &chatv1.NewsEvent_Snapshot{Snapshot: snap}}

	case ev.Digest != nil:
		return &chatv1.NewsEvent{Event: &chatv1.NewsEvent_Digest{Digest: newsDigest(ev.Digest)}}

	case ev.State != nil:
		return &chatv1.NewsEvent{Event: &chatv1.NewsEvent_State{State: scanState(*ev.State)}}

	case ev.Err != nil:
		// The message is deliberately generic. A scan failure is an upstream
		// provider problem, and this endpoint is public — the detail belongs in
		// the log, not on a stranger's screen.
		return &chatv1.NewsEvent{Event: &chatv1.NewsEvent_Error{Error: &chatv1.NewsError{
			Code:    "scan_failed",
			Message: "the news scan could not be completed — showing the last result",
		}}}
	}
	return nil
}

func scanState(s newswatch.State) chatv1.ScanState {
	if s == newswatch.StateScanning {
		return chatv1.ScanState_SCAN_STATE_SCANNING
	}
	return chatv1.ScanState_SCAN_STATE_IDLE
}

func newsDigest(d *newsagent.Digest) *chatv1.NewsDigest {
	if d == nil {
		return nil
	}
	items := make([]*chatv1.NewsItem, 0, len(d.Items))
	for _, it := range d.Items {
		items = append(items, &chatv1.NewsItem{
			Id:        it.ID,
			Topic:     it.Topic,
			Headline:  it.Headline,
			Summary:   it.Summary,
			Url:       it.URL,
			Source:    it.Source,
			Published: it.Published,
		})
	}
	return &chatv1.NewsDigest{
		Id:              d.ID,
		GeneratedAtUnix: d.GeneratedAt.Unix(),
		Items:           items,
		ToolCalls:       int32(d.ToolCalls),
		TotalTokens:     d.TotalTokens,
	}
}
